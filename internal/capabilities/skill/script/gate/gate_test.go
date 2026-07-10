package gate_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"genesis-agent/internal/capabilities/skill/script/gate"
)

func TestCheckDeliveryRejectsFakePPTX(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.pptx")
	if err := os.WriteFile(path, []byte("PPT file content"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, kind, reason := gate.CheckDelivery(path)
	if ok || kind != "pptx" {
		t.Fatalf("ok=%v kind=%s reason=%s", ok, kind, reason)
	}
	if reason == "" {
		t.Fatal("expected reason")
	}
}

func TestCheckDeliveryAcceptsMinimalOOXML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.pptx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("[Content_Types].xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`<?xml version="1.0"?><Types></Types>`)); err != nil {
		t.Fatal(err)
	}
	w, err = zw.Create("ppt/slides/slide1.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`<sld/>`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	ok, kind, reason := gate.CheckDelivery(path)
	if !ok || kind != "pptx" {
		t.Fatalf("ok=%v kind=%s reason=%s", ok, kind, reason)
	}
}
