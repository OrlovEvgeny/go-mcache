package radix

import (
	"fmt"
	"testing"
)

func TestTreeBasicOperations(t *testing.T) {
	tree := New()

	// Test Insert and Has
	tree.Insert("hello", 1)
	tree.Insert("world", 2)
	tree.Insert("help", 3)

	if !tree.Has("hello") {
		t.Error("Expected 'hello' to exist")
	}
	if !tree.Has("world") {
		t.Error("Expected 'world' to exist")
	}
	if !tree.Has("help") {
		t.Error("Expected 'help' to exist")
	}
	if tree.Has("he") {
		t.Error("Expected 'he' to not exist")
	}
	if tree.Has("hellox") {
		t.Error("Expected 'hellox' to not exist")
	}

	// Test Size
	if tree.Size() != 3 {
		t.Errorf("Expected size 3, got %d", tree.Size())
	}

	// Test Delete
	tree.Delete("help")
	if tree.Has("help") {
		t.Error("Expected 'help' to be deleted")
	}
	if tree.Size() != 2 {
		t.Errorf("Expected size 2 after delete, got %d", tree.Size())
	}

	// Test Clear
	tree.Clear()
	if tree.Size() != 0 {
		t.Error("Expected size 0 after clear")
	}
}

func TestTreePrefixSearch(t *testing.T) {
	tree := New()

	// Insert keys with common prefixes
	keys := []string{
		"user:1",
		"user:2",
		"user:10",
		"user:100",
		"order:1",
		"order:2",
		"product:1",
	}
	for i, k := range keys {
		tree.Insert(k, uint64(i))
	}

	// Find by prefix "user:"
	results := tree.FindByPrefix("user:", 100)
	if len(results) != 4 {
		t.Errorf("Expected 4 results for 'user:' prefix, got %d", len(results))
	}

	// Find by prefix "order:"
	results = tree.FindByPrefix("order:", 100)
	if len(results) != 2 {
		t.Errorf("Expected 2 results for 'order:' prefix, got %d", len(results))
	}

	// Find by prefix "prod"
	results = tree.FindByPrefix("prod", 100)
	if len(results) != 1 {
		t.Errorf("Expected 1 result for 'prod' prefix, got %d", len(results))
	}

	// Find by non-existent prefix
	results = tree.FindByPrefix("xyz", 100)
	if len(results) != 0 {
		t.Errorf("Expected 0 results for 'xyz' prefix, got %d", len(results))
	}
}

func TestTreeWalkPrefix(t *testing.T) {
	tree := New()

	tree.Insert("apple", 1)
	tree.Insert("application", 2)
	tree.Insert("apply", 3)
	tree.Insert("banana", 4)

	var found []string
	tree.WalkPrefix("app", func(key string, hash uint64) bool {
		found = append(found, key)
		return true
	})

	if len(found) != 3 {
		t.Errorf("Expected 3 keys with 'app' prefix, got %d: %v", len(found), found)
	}

	// Test early termination
	found = nil
	tree.WalkPrefix("app", func(key string, hash uint64) bool {
		found = append(found, key)
		return len(found) < 2 // Stop after 2
	})

	if len(found) != 2 {
		t.Errorf("Expected walk to stop after 2, got %d", len(found))
	}
}

func TestTreeLimit(t *testing.T) {
	tree := New()

	// Insert many keys
	for i := 0; i < 100; i++ {
		tree.Insert(fmt.Sprintf("key%03d", i), uint64(i))
	}

	// Find with limit
	results := tree.FindByPrefix("key", 10)
	if len(results) > 10 {
		t.Errorf("Expected at most 10 results, got %d", len(results))
	}
}

func TestTreeEdgeCases(t *testing.T) {
	tree := New()

	// Empty string key
	tree.Insert("", 0)
	if !tree.Has("") {
		t.Error("Expected empty string key to exist")
	}

	// Single character keys
	tree.Insert("a", 1)
	tree.Insert("b", 2)
	if !tree.Has("a") || !tree.Has("b") {
		t.Error("Single char keys should exist")
	}

	// Long common prefix
	tree.Insert("abcdefghijklmnop", 10)
	tree.Insert("abcdefghijklmnopqrstuvwxyz", 20)
	tree.Insert("abcdefghijklmnoq", 30)

	if !tree.Has("abcdefghijklmnop") {
		t.Error("Long key 1 should exist")
	}
	if !tree.Has("abcdefghijklmnopqrstuvwxyz") {
		t.Error("Long key 2 should exist")
	}
}

func TestTreeDeleteNonExistent(t *testing.T) {
	tree := New()

	tree.Insert("exists", 1)

	// Delete non-existent key
	deleted := tree.Delete("nonexistent")
	if deleted {
		t.Error("Delete should return false for non-existent key")
	}

	// Delete existing key
	deleted = tree.Delete("exists")
	if !deleted {
		t.Error("Delete should return true for existing key")
	}

	// Delete same key again
	deleted = tree.Delete("exists")
	if deleted {
		t.Error("Delete should return false when deleting already deleted key")
	}
}

func BenchmarkTreeInsert(b *testing.B) {
	tree := New()
	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = fmt.Sprintf("key%010d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Insert(keys[i], uint64(i))
	}
}

func BenchmarkTreeFindByPrefix(b *testing.B) {
	tree := New()

	// Pre-populate with many keys
	for i := 0; i < 100000; i++ {
		tree.Insert(fmt.Sprintf("user:%d:profile", i), uint64(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.FindByPrefix("user:", 100)
	}
}

func BenchmarkTreeHas(b *testing.B) {
	tree := New()
	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = fmt.Sprintf("key%010d", i)
		tree.Insert(keys[i], uint64(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Has(keys[i%len(keys)])
	}
}
