// Package glob provides glob pattern matching for cache keys.
package glob

import (
	"errors"
	"strings"
)

// Pattern represents a compiled glob pattern.
type Pattern struct {
	raw       string
	prefix    string // Literal prefix before first wildcard
	segments  []segment
	hasDouble bool // Contains **
}

type segment struct {
	typ     segmentType
	literal string
	charset string // For [abc] patterns
	negated bool   // For [^abc] patterns
}

type segmentType int

const (
	segLiteral  segmentType = iota // Exact string match
	segStar                        // * - matches any characters except separator
	segDouble                      // ** - matches any characters including separator
	segQuestion                    // ? - matches single character
	segCharset                     // [abc] - matches one character in set
)

var (
	ErrEmptyPattern   = errors.New("empty pattern")
	ErrInvalidPattern = errors.New("invalid pattern")
	ErrUnmatchedBrace = errors.New("unmatched bracket in pattern")
)

// Compile compiles a glob pattern string into a Pattern.
// Supported patterns:
//   - * matches any sequence of characters (except path separator in some modes)
//   - ** matches any sequence including path separators
//   - ? matches any single character
//   - [abc] matches any character in the set
//   - [^abc] matches any character not in the set
//   - [a-z] matches any character in the range
func Compile(pattern string) (*Pattern, error) {
	if pattern == "" {
		return nil, ErrEmptyPattern
	}

	p := &Pattern{
		raw:      pattern,
		segments: make([]segment, 0),
	}

	// Find literal prefix
	p.prefix = extractPrefix(pattern)

	// Parse the pattern into segments
	i := 0
	for i < len(pattern) {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				p.segments = append(p.segments, segment{typ: segDouble})
				p.hasDouble = true
				i += 2
			} else {
				p.segments = append(p.segments, segment{typ: segStar})
				i++
			}

		case '?':
			p.segments = append(p.segments, segment{typ: segQuestion})
			i++

		case '[':
			charset, negated, end, err := parseCharset(pattern, i)
			if err != nil {
				return nil, err
			}
			p.segments = append(p.segments, segment{
				typ:     segCharset,
				charset: charset,
				negated: negated,
			})
			i = end

		default:
			// Literal segment - collect consecutive literal characters
			start := i
			for i < len(pattern) && pattern[i] != '*' && pattern[i] != '?' && pattern[i] != '[' {
				if pattern[i] == '\\' && i+1 < len(pattern) {
					i++ // Skip escaped character
				}
				i++
			}
			p.segments = append(p.segments, segment{
				typ:     segLiteral,
				literal: unescape(pattern[start:i]),
			})
		}
	}

	return p, nil
}

// extractPrefix returns the literal prefix before any wildcard.
func extractPrefix(pattern string) string {
	var prefix strings.Builder
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '*' || c == '?' || c == '[' {
			break
		}
		if c == '\\' && i+1 < len(pattern) {
			prefix.WriteByte(pattern[i+1])
			i++
			continue
		}
		prefix.WriteByte(c)
	}
	return prefix.String()
}

// parseCharset parses a [charset] pattern starting at position i.
// Returns the expanded charset, whether it's negated, and the ending position.
func parseCharset(pattern string, i int) (string, bool, int, error) {
	if i >= len(pattern) || pattern[i] != '[' {
		return "", false, i, ErrInvalidPattern
	}
	i++

	negated := false
	if i < len(pattern) && (pattern[i] == '^' || pattern[i] == '!') {
		negated = true
		i++
	}

	var charset strings.Builder
	for i < len(pattern) && pattern[i] != ']' {
		if pattern[i] == '\\' && i+1 < len(pattern) {
			charset.WriteByte(pattern[i+1])
			i += 2
			continue
		}

		// Check for range (a-z)
		if i+2 < len(pattern) && pattern[i+1] == '-' && pattern[i+2] != ']' {
			start := pattern[i]
			end := pattern[i+2]
			if start > end {
				start, end = end, start
			}
			for c := start; c <= end; c++ {
				charset.WriteByte(c)
			}
			i += 3
			continue
		}

		charset.WriteByte(pattern[i])
		i++
	}

	if i >= len(pattern) {
		return "", false, i, ErrUnmatchedBrace
	}

	return charset.String(), negated, i + 1, nil
}

// unescape removes backslash escapes from a string.
func unescape(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}

	var result strings.Builder
	result.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			result.WriteByte(s[i+1])
			i++
		} else {
			result.WriteByte(s[i])
		}
	}
	return result.String()
}

// Match checks if the key matches the pattern.
//
// Matching is iterative with single-star backtracking (the classic greedy
// wildcard algorithm): worst case O(len(key) * len(segments)). The previous
// recursive implementation backtracked exponentially on patterns with
// several stars (e.g. "*a*a*a*x"), which is a CPU DoS with adversarial
// patterns. * and ** are equivalent: keys have no path semantics here.
func (p *Pattern) Match(key string) bool {
	segs := p.segments
	si, ki := 0, 0
	starSeg, starKey := -1, 0 // most recent star segment and the key pos after it

	for {
		if si < len(segs) {
			seg := &segs[si]
			switch seg.typ {
			case segStar, segDouble:
				// Greedily match zero characters; on later mismatch,
				// backtrack here and consume one more.
				starSeg = si
				starKey = ki
				si++
				continue

			case segLiteral:
				if len(seg.literal) <= len(key)-ki && key[ki:ki+len(seg.literal)] == seg.literal {
					ki += len(seg.literal)
					si++
					continue
				}

			case segQuestion:
				if ki < len(key) {
					ki++
					si++
					continue
				}

			case segCharset:
				if ki < len(key) && seg.matchesByte(key[ki]) {
					ki++
					si++
					continue
				}
			}
		} else if ki == len(key) {
			return true
		}

		// Mismatch: let the last star swallow one more character.
		if starSeg == -1 || starKey >= len(key) {
			return false
		}
		starKey++
		ki = starKey
		si = starSeg + 1
	}
}

// matchesByte reports whether c satisfies a charset segment.
func (s *segment) matchesByte(c byte) bool {
	inSet := strings.IndexByte(s.charset, c) >= 0
	if s.negated {
		return !inSet
	}
	return inSet
}

// Prefix returns the literal prefix of the pattern.
// This can be used to optimize searches using a radix tree.
func (p *Pattern) Prefix() string {
	return p.prefix
}

// Raw returns the original pattern string.
func (p *Pattern) Raw() string {
	return p.raw
}

// HasDoubleStar returns true if the pattern contains **.
func (p *Pattern) HasDoubleStar() bool {
	return p.hasDouble
}

// IsLiteral returns true if the pattern has no wildcards.
func (p *Pattern) IsLiteral() bool {
	return len(p.segments) == 1 && p.segments[0].typ == segLiteral
}

// MustCompile compiles a pattern and panics on error.
func MustCompile(pattern string) *Pattern {
	p, err := Compile(pattern)
	if err != nil {
		panic(err)
	}
	return p
}

// Match is a convenience function that compiles and matches in one call.
func Match(pattern, key string) (bool, error) {
	p, err := Compile(pattern)
	if err != nil {
		return false, err
	}
	return p.Match(key), nil
}
