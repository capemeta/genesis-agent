package write_file

import "testing"

func TestComposeWriteContentAppendDoesNotMutateInputs(t *testing.T) {
	existing := []byte("first")
	incoming := []byte("-second")
	got := composeWriteContent(existing, incoming, true)
	if string(got) != "first-second" {
		t.Fatalf("got %q", got)
	}
	got[0] = 'F'
	if string(existing) != "first" || string(incoming) != "-second" {
		t.Fatal("compose must not alias input buffers")
	}
}

func TestComposeWriteContentOverwrite(t *testing.T) {
	incoming := []byte("new")
	if got := composeWriteContent([]byte("old"), incoming, false); string(got) != "new" {
		t.Fatalf("got %q", got)
	}
}
