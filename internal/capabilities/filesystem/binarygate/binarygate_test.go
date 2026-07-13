package binarygate_test

import (
	"testing"

	"genesis-agent/internal/capabilities/filesystem/binarygate"
)

func TestRejectFakeOfficeBinary(t *testing.T) {
	if err := binarygate.RejectFakeOfficeBinary("a.pptx", []byte("plain text")); err == nil {
		t.Fatal("expected reject")
	}
	if err := binarygate.RejectFakeOfficeBinary("a.pptx", []byte("PK\x03\x04rest")); err != nil {
		t.Fatal(err)
	}
	if err := binarygate.RejectFakeOfficeBinary("a.pdf", []byte("not pdf")); err == nil {
		t.Fatal("expected reject")
	}
	if err := binarygate.RejectFakeOfficeBinary("a.pdf", []byte("%PDF-1.4")); err != nil {
		t.Fatal(err)
	}
	if err := binarygate.RejectFakeOfficeBinary("a.txt", []byte("ok")); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyContentTreatsOfficeAsBinary(t *testing.T) {
	class := binarygate.ClassifyContent("deck.pptx", []byte("PK\x03\x04"))
	if !class.Binary {
		t.Fatal("pptx should be classified as binary")
	}
	if class.MIME != "application/vnd.openxmlformats-officedocument.presentationml.presentation" {
		t.Fatalf("mime=%q", class.MIME)
	}
}

func TestClassifyContentKeepsUTF8TextReadable(t *testing.T) {
	class := binarygate.ClassifyContent("notes.md", []byte("# title\n中文内容\n"))
	if class.Binary {
		t.Fatalf("text should not be classified as binary, mime=%q", class.MIME)
	}
}

func TestClassifyContentTreatsInvalidUTF8AsBinary(t *testing.T) {
	class := binarygate.ClassifyContent("blob.dat", []byte{0xff, 0xfe, 0xfd})
	if !class.Binary {
		t.Fatal("invalid utf-8 should be classified as binary")
	}
	if class.MIME != "application/octet-stream" {
		t.Fatalf("mime=%q", class.MIME)
	}
}
