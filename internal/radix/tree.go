// Package radix provides a radix tree for efficient prefix search.
package radix

import (
	"sync"
)

// Tree is a thread-safe radix tree for string keys.
type Tree struct {
	mu   sync.RWMutex
	root *node
	size int64
}

// node represents a node in the radix tree.
type node struct {
	prefix   string
	children map[byte]*node
	isLeaf   bool
	keyHash  uint64
}

// New creates a new radix tree.
func New() *Tree {
	return &Tree{
		root: &node{
			children: make(map[byte]*node),
		},
	}
}

// Insert adds a key to the tree with its associated hash.
func (t *Tree) Insert(key string, hash uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.insertNode(t.root, key, hash)
	t.size++
}

// insertNode recursively inserts a key into the tree.
func (t *Tree) insertNode(n *node, key string, hash uint64) {
	if len(key) == 0 {
		n.isLeaf = true
		n.keyHash = hash
		return
	}

	c := key[0]
	child, exists := n.children[c]

	if !exists {
		// Create new child with remaining key
		n.children[c] = &node{
			prefix:   key,
			children: make(map[byte]*node),
			isLeaf:   true,
			keyHash:  hash,
		}
		return
	}

	// Find common prefix length
	commonLen := commonPrefixLen(child.prefix, key)

	if commonLen == len(child.prefix) {
		// Key starts with child's prefix, recurse
		t.insertNode(child, key[commonLen:], hash)
		return
	}

	// Split the child node
	// Create new node with common prefix
	newChild := &node{
		prefix:   child.prefix[:commonLen],
		children: make(map[byte]*node),
	}

	// Move existing child under new node
	child.prefix = child.prefix[commonLen:]
	newChild.children[child.prefix[0]] = child

	// Add new key under new node
	remaining := key[commonLen:]
	if len(remaining) == 0 {
		newChild.isLeaf = true
		newChild.keyHash = hash
	} else {
		newChild.children[remaining[0]] = &node{
			prefix:   remaining,
			children: make(map[byte]*node),
			isLeaf:   true,
			keyHash:  hash,
		}
	}

	n.children[c] = newChild
}

// Delete removes a key from the tree.
func (t *Tree) Delete(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	deleted := t.deleteNode(t.root, key)
	if deleted {
		t.size--
	}
	return deleted
}

// deleteNode recursively deletes a key from the tree.
func (t *Tree) deleteNode(n *node, key string) bool {
	if len(key) == 0 {
		if n.isLeaf {
			n.isLeaf = false
			n.keyHash = 0
			return true
		}
		return false
	}

	c := key[0]
	child, exists := n.children[c]
	if !exists {
		return false
	}

	commonLen := commonPrefixLen(child.prefix, key)
	if commonLen < len(child.prefix) {
		// Key doesn't exist
		return false
	}

	deleted := t.deleteNode(child, key[commonLen:])

	// Clean up empty nodes
	if !child.isLeaf && len(child.children) == 0 {
		delete(n.children, c)
	} else if !child.isLeaf && len(child.children) == 1 {
		// Merge single child with parent
		for _, grandchild := range child.children {
			grandchild.prefix = child.prefix + grandchild.prefix
			n.children[c] = grandchild
			break
		}
	}

	return deleted
}

// FindByPrefix returns up to limit key hashes that have the given prefix.
func (t *Tree) FindByPrefix(prefix string, limit int) []uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if limit <= 0 {
		limit = 100 // Default limit
	}

	// Find the node that matches the prefix
	n := t.root
	remaining := prefix

	for len(remaining) > 0 {
		c := remaining[0]
		child, exists := n.children[c]
		if !exists {
			return nil
		}

		commonLen := commonPrefixLen(child.prefix, remaining)
		if commonLen < len(remaining) && commonLen < len(child.prefix) {
			// Prefix doesn't match
			return nil
		}

		if commonLen >= len(remaining) {
			// Found node containing prefix
			n = child
			break
		}

		n = child
		remaining = remaining[commonLen:]
	}

	// Collect all keys under this node
	results := make([]uint64, 0, limit)
	t.collectHashes(n, &results, limit)
	return results
}

// collectHashes collects key hashes from a node and its children.
func (t *Tree) collectHashes(n *node, results *[]uint64, limit int) {
	if len(*results) >= limit {
		return
	}

	if n.isLeaf {
		*results = append(*results, n.keyHash)
	}

	for _, child := range n.children {
		t.collectHashes(child, results, limit)
		if len(*results) >= limit {
			return
		}
	}
}

// WalkPrefix calls fn for each key with the given prefix.
// If fn returns false, the walk stops.
func (t *Tree) WalkPrefix(prefix string, fn func(key string, hash uint64) bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Find the node that matches the prefix
	n := t.root
	remaining := prefix
	keyBuilder := ""

	for len(remaining) > 0 {
		c := remaining[0]
		child, exists := n.children[c]
		if !exists {
			return
		}

		commonLen := commonPrefixLen(child.prefix, remaining)
		if commonLen < len(remaining) && commonLen < len(child.prefix) {
			// Prefix doesn't match
			return
		}

		keyBuilder += child.prefix
		if commonLen >= len(remaining) {
			// Found node containing prefix
			n = child
			break
		}

		n = child
		remaining = remaining[commonLen:]
	}

	// Walk all keys under this node
	t.walkNode(n, prefix, fn)
}

// walkNode recursively walks a node and its children.
func (t *Tree) walkNode(n *node, keyPrefix string, fn func(key string, hash uint64) bool) bool {
	if n.isLeaf {
		if !fn(keyPrefix, n.keyHash) {
			return false
		}
	}

	for _, child := range n.children {
		if !t.walkNode(child, keyPrefix+child.prefix, fn) {
			return false
		}
	}

	return true
}

// Has checks if a key exists in the tree.
func (t *Tree) Has(key string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	n := t.root
	remaining := key

	for len(remaining) > 0 {
		c := remaining[0]
		child, exists := n.children[c]
		if !exists {
			return false
		}

		commonLen := commonPrefixLen(child.prefix, remaining)
		if commonLen < len(child.prefix) {
			return false
		}

		if commonLen == len(remaining) {
			return child.isLeaf
		}

		n = child
		remaining = remaining[commonLen:]
	}

	return n.isLeaf
}

// Size returns the number of keys in the tree.
func (t *Tree) Size() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.size
}

// Clear removes all keys from the tree.
func (t *Tree) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.root = &node{
		children: make(map[byte]*node),
	}
	t.size = 0
}

// commonPrefixLen returns the length of the common prefix between two strings.
func commonPrefixLen(a, b string) int {
	maxLen := len(a)
	if len(b) < maxLen {
		maxLen = len(b)
	}
	for i := 0; i < maxLen; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return maxLen
}
