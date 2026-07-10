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
