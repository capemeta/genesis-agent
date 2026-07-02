package toolkit

import "testing"

func TestDecodeParamsRejectsUnknownFields(t *testing.T) {
	var in struct {
		Path string `json:"path"`
	}
	err := DecodeParams(`{"path":"a.txt","unexpected":true}`, &in)
	if err == nil {
		t.Fatal("DecodeParams error = nil, want unknown field error")
	}
}

func TestDecodeParamsRejectsMultipleObjects(t *testing.T) {
	var in struct {
		Path string `json:"path"`
	}
	err := DecodeParams(`{"path":"a.txt"} {"path":"b.txt"}`, &in)
	if err == nil {
		t.Fatal("DecodeParams error = nil, want multiple object error")
	}
}

func TestDecodeParamsAcceptsExpectedFields(t *testing.T) {
	var in struct {
		Path string `json:"path"`
	}
	if err := DecodeParams(`{"path":"a.txt"}`, &in); err != nil {
		t.Fatalf("DecodeParams error = %v", err)
	}
	if in.Path != "a.txt" {
		t.Fatalf("Path = %q, want a.txt", in.Path)
	}
}
