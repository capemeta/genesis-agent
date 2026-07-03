package patch

import "testing"

func TestParseMultipleOperations(t *testing.T) {
	hunks, err := Parse("*** Begin Patch\n*** Add File: nested/new.txt\n+created\n*** Delete File: old.txt\n*** Update File: modify.txt\n@@\n-old\n+new\n*** End Patch")
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 3 {
		t.Fatalf("len(hunks)=%d, want 3", len(hunks))
	}
	if hunks[0].Type != HunkAdd || hunks[1].Type != HunkDelete || hunks[2].Type != HunkUpdate {
		t.Fatalf("unexpected hunks: %#v", hunks)
	}
}

func TestParseMove(t *testing.T) {
	hunks, err := Parse("*** Begin Patch\n*** Update File: old/name.txt\n*** Move to: renamed/name.txt\n@@\n-old\n+new\n*** End Patch")
	if err != nil {
		t.Fatal(err)
	}
	if hunks[0].MovePath != "renamed/name.txt" {
		t.Fatalf("MovePath=%q", hunks[0].MovePath)
	}
}

func TestDeriveNewContentMultipleChunks(t *testing.T) {
	next, err := deriveNewContent("multi.txt", "line1\nline2\nline3\nline4\n", []Chunk{
		{OldLines: []string{"line2"}, NewLines: []string{"changed2"}},
		{OldLines: []string{"line4"}, NewLines: []string{"changed4"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "line1\nchanged2\nline3\nchanged4\n"
	if next != want {
		t.Fatalf("next=%q, want %q", next, want)
	}
}

func TestDeriveNewContentMissingContext(t *testing.T) {
	_, err := deriveNewContent("modify.txt", "line1\nline2\n", []Chunk{{OldLines: []string{"missing"}, NewLines: []string{"changed"}}})
	if err == nil {
		t.Fatal("err=nil, want missing context error")
	}
}

func TestDeriveNewContentAppendsTrailingNewline(t *testing.T) {
	next, err := deriveNewContent("no_newline.txt", "no newline at end", []Chunk{{OldLines: []string{"no newline at end"}, NewLines: []string{"first line", "second line"}}})
	if err != nil {
		t.Fatal(err)
	}
	if next != "first line\nsecond line\n" {
		t.Fatalf("next=%q", next)
	}
}

func TestDeriveNewContentUnicodeDash(t *testing.T) {
	original := "import asyncio  # local import \u2013 avoids top\u2011level dep\n"
	next, err := deriveNewContent("unicode.py", original, []Chunk{{OldLines: []string{"import asyncio  # local import - avoids top-level dep"}, NewLines: []string{"import asyncio  # HELLO"}}})
	if err != nil {
		t.Fatal(err)
	}
	if next != "import asyncio  # HELLO\n" {
		t.Fatalf("next=%q", next)
	}
}

func TestDeriveNewContentInsertAtEOF(t *testing.T) {
	next, err := deriveNewContent("insert.txt", "foo\nbar\n", []Chunk{{NewLines: []string{"baz"}, EndOfFile: true}})
	if err != nil {
		t.Fatal(err)
	}
	if next != "foo\nbar\nbaz\n" {
		t.Fatalf("next=%q", next)
	}
}
