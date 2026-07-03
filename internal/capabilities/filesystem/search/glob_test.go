package search

import "testing"

func TestGlobMatcherDoubleStar(t *testing.T) {
	m, err := NewGlobMatcher("**/*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"main.go", "internal/app/app.go"} {
		if !m.Match(p) {
			t.Fatalf("%q should match", p)
		}
	}
	if m.Match("main.ts") {
		t.Fatal("main.ts should not match")
	}
}

func TestGlobMatcherBasename(t *testing.T) {
	m, err := NewGlobMatcher("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if !m.Match("a/b/main.go") {
		t.Fatal("basename should match")
	}
}
