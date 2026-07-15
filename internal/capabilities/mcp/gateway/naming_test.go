package gateway

import "testing"

func TestModelToolName(t *testing.T) {
	got := ModelToolName("file system", "read/file")
	want := "mcp__file_system__read_file"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDeduperUnique(t *testing.T) {
	d := NewDeduper()
	a := d.Unique("s", "t")
	b := d.Unique("s", "t")
	if a == b {
		t.Fatalf("expected unique names, got %q twice", a)
	}
	if a != "mcp__s__t" {
		t.Fatalf("first name = %q", a)
	}
}
