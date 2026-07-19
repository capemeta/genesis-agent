package extract_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"genesis-agent/internal/capabilities/turninput/extract"
)

func TestDocxExtract(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.docx")
	if err := writeMinimalDocx(path, "Hello Genesis Docx"); err != nil {
		t.Fatal(err)
	}
	text, err := extract.Docx{}.Extract(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Hello Genesis Docx") {
		t.Fatalf("got %q", text)
	}
}

func TestPDFExtractLiterals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.pdf")
	// 最小可读文本 PDF（非压缩字面量）
	content := "%PDF-1.1\n1 0 obj<<>>endobj\nBT (Hello PDF Layer) Tj ET\n%%EOF\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := extract.PDF{}.Extract(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Hello PDF Layer") {
		t.Fatalf("got %q", text)
	}
}

func writeMinimalDocx(path, body string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		return err
	}
	xml := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body><w:p><w:r><w:t>` + body + `</w:t></w:r></w:p></w:body></w:document>`
	if _, err := w.Write([]byte(xml)); err != nil {
		return err
	}
	return zw.Close()
}
