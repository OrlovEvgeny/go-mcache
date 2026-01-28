package glob

import "testing"

func TestPatternCompile(t *testing.T) {
	tests := []struct {
		pattern string
		prefix  string
		isLit   bool
		err     bool
	}{
		{"hello", "hello", true, false},
		{"hello*", "hello", false, false},
		{"*world", "", false, false},
		{"he?lo", "he", false, false},
		{"[abc]def", "", false, false},
		{"user:*", "user:", false, false},
		{"**", "", false, false},
		{"a**b", "a", false, false},
		{"", "", false, true}, // Empty pattern error
	}

	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			p, err := Compile(tc.pattern)
			if tc.err {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if p.Prefix() != tc.prefix {
				t.Errorf("Expected prefix %q, got %q", tc.prefix, p.Prefix())
			}
			if p.IsLiteral() != tc.isLit {
				t.Errorf("Expected IsLiteral=%v, got %v", tc.isLit, p.IsLiteral())
			}
		})
	}
}

func TestPatternMatch(t *testing.T) {
	tests := []struct {
		pattern string
		key     string
		match   bool
	}{
		// Literal matches
		{"hello", "hello", true},
		{"hello", "world", false},
		{"hello", "hello!", false},
		{"hello", "hell", false},

		// Star wildcard
		{"*", "", true},
		{"*", "anything", true},
		{"hello*", "hello", true},
		{"hello*", "helloworld", true},
		{"hello*", "hello world", true},
		{"*world", "world", true},
		{"*world", "hello world", true},
		{"*world", "worldx", false},
		{"hello*world", "helloworld", true},
		{"hello*world", "hello big world", true},
		{"hello*world", "hello world!", false},
		{"*a*b*", "aabb", true},
		{"*a*b*", "xaxbx", true},

		// Question wildcard
		{"?", "a", true},
		{"?", "", false},
		{"?", "ab", false},
		{"h?llo", "hello", true},
		{"h?llo", "hallo", true},
		{"h?llo", "hllo", false},
		{"???", "abc", true},
		{"???", "ab", false},

		// Character class
		{"[abc]", "a", true},
		{"[abc]", "b", true},
		{"[abc]", "c", true},
		{"[abc]", "d", false},
		{"[a-z]", "m", true},
		{"[a-z]", "A", false},
		{"[0-9]", "5", true},
		{"[^abc]", "d", true},
		{"[^abc]", "a", false},
		{"[!abc]", "d", true},
		{"[!abc]", "b", false},

		// Double star
		{"**", "", true},
		{"**", "anything/anywhere", true},
		{"a**b", "ab", true},
		{"a**b", "axxxb", true},
		{"a**b", "a/path/to/b", true},

		// Combined patterns
		{"user:*:name", "user:123:name", true},
		{"user:*:name", "user::name", true},
		{"user:*:name", "user:name", false},
		{"[a-z]*[0-9]", "abc123", true},  // ends with 3, which is in [0-9]
		{"[a-z]*[0-9]", "a1", true},
		{"*.txt", "file.txt", true},
		{"*.txt", "file.tar.txt", true},
		{"*.txt", "filetxt", false},

		// Edge cases
		{"", "", false}, // Empty pattern won't compile, but test anyway
		{"*", "", true},
		{"?", "", false},
		{"[a]", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.pattern+"_"+tc.key, func(t *testing.T) {
			if tc.pattern == "" {
				return // Skip empty pattern
			}

			p, err := Compile(tc.pattern)
			if err != nil {
				t.Skipf("Pattern compile error: %v", err)
				return
			}

			result := p.Match(tc.key)
			if result != tc.match {
				t.Errorf("Pattern %q matching %q: expected %v, got %v",
					tc.pattern, tc.key, tc.match, result)
			}
		})
	}
}

func TestPatternPrefix(t *testing.T) {
	tests := []struct {
		pattern string
		prefix  string
	}{
		{"hello", "hello"},
		{"hello*", "hello"},
		{"hello*world", "hello"},
		{"*world", ""},
		{"?ello", ""},
		{"[abc]ello", ""},
		{"user:*:profile", "user:"},
		{"user:123:*", "user:123:"},
		{"**", ""},
	}

	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			p, err := Compile(tc.pattern)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}

			if p.Prefix() != tc.prefix {
				t.Errorf("Expected prefix %q, got %q", tc.prefix, p.Prefix())
			}
		})
	}
}

func TestPatternEscape(t *testing.T) {
	tests := []struct {
		pattern string
		key     string
		match   bool
	}{
		{`hello\*world`, "hello*world", true},
		{`hello\*world`, "helloXworld", false},
		{`\?`, "?", true},
		{`\?`, "a", false},
		{`\[abc\]`, "[abc]", true},
		{`a\\b`, `a\b`, true},
	}

	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			p, err := Compile(tc.pattern)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}

			result := p.Match(tc.key)
			if result != tc.match {
				t.Errorf("Pattern %q matching %q: expected %v, got %v",
					tc.pattern, tc.key, tc.match, result)
			}
		})
	}
}

func TestPatternCharRange(t *testing.T) {
	tests := []struct {
		pattern string
		key     string
		match   bool
	}{
		{"[a-z]", "a", true},
		{"[a-z]", "z", true},
		{"[a-z]", "m", true},
		{"[a-z]", "A", false},
		{"[a-z]", "1", false},
		{"[A-Z]", "M", true},
		{"[A-Z]", "m", false},
		{"[0-9]", "0", true},
		{"[0-9]", "9", true},
		{"[0-9]", "5", true},
		{"[0-9]", "a", false},
		{"[a-zA-Z]", "a", true},
		{"[a-zA-Z]", "Z", true},
		{"[a-zA-Z]", "5", false},
		{"[a-zA-Z0-9]", "a", true},
		{"[a-zA-Z0-9]", "5", true},
		{"[a-zA-Z0-9]", "_", false},
	}

	for _, tc := range tests {
		t.Run(tc.pattern+"_"+tc.key, func(t *testing.T) {
			p, err := Compile(tc.pattern)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}

			result := p.Match(tc.key)
			if result != tc.match {
				t.Errorf("Pattern %q matching %q: expected %v, got %v",
					tc.pattern, tc.key, tc.match, result)
			}
		})
	}
}

func TestMustCompilePanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected MustCompile to panic on empty pattern")
		}
	}()

	MustCompile("")
}

func TestMatchFunction(t *testing.T) {
	match, err := Match("hello*", "hello world")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !match {
		t.Error("Expected match")
	}

	match, err = Match("hello*", "world")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if match {
		t.Error("Expected no match")
	}

	_, err = Match("", "anything")
	if err == nil {
		t.Error("Expected error for empty pattern")
	}
}

func BenchmarkPatternMatchLiteral(b *testing.B) {
	p := MustCompile("hello")
	key := "hello"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Match(key)
	}
}

func BenchmarkPatternMatchStar(b *testing.B) {
	p := MustCompile("hello*world")
	key := "hello big beautiful world"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Match(key)
	}
}

func BenchmarkPatternMatchComplex(b *testing.B) {
	p := MustCompile("user:*:[a-z]*:profile")
	key := "user:12345:admin:profile"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Match(key)
	}
}

func BenchmarkPatternCompile(b *testing.B) {
	pattern := "user:*:[a-z0-9]*:profile"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Compile(pattern)
	}
}
