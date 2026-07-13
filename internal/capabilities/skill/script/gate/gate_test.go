package gate_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
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
	if _, err := w.Write([]byte(`<p:sld><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Hello</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`)); err != nil {
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

func TestCheckDeliveryRejectsBlankPPTXSlides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blank.pptx")
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
	// 模拟误用 addSlide({title:...}) 生成的空幻灯片：只有空 spTree，无文本/形状
	blank := `<?xml version="1.0"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/></p:spTree></p:cSld></p:sld>`
	if _, err := w.Write([]byte(blank)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	ok, kind, reason := gate.CheckDelivery(path)
	if ok || kind != "pptx" {
		t.Fatalf("ok=%v kind=%s reason=%s", ok, kind, reason)
	}
	if !strings.Contains(reason, "空白") {
		t.Fatalf("expected blank-slide reason, got %q", reason)
	}
}
