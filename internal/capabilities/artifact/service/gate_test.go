package service

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestBasicGateRejectsOfficeContainerWithoutRequiredStructure(t *testing.T) {
	file := zipFile(t, map[string]string{"notes.txt": "not a presentation"})
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = (BasicGate{}).Validate(context.Background(), "deck.pptx", "", info.Size(), file)
	if err == nil || !strings.Contains(err.Error(), "ppt/presentation.xml") {
		t.Fatalf("应拒绝伪造 pptx，实际: %v", err)
	}
}

func TestBasicGateAcceptsStructurallyValidOfficeContainer(t *testing.T) {
	file := zipFile(t, map[string]string{"[Content_Types].xml": "<Types/>", "ppt/presentation.xml": "<p:presentation/>"})
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	kind, _, err := (BasicGate{}).Validate(context.Background(), "deck.pptx", "", info.Size(), file)
	if err != nil || kind != "pptx" {
		t.Fatalf("有效 pptx 容器应通过，kind=%s err=%v", kind, err)
	}
}

func TestBasicGateRejectsPDFWithoutEOF(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "invalid-*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("%PDF-1.7\nobject without trailer"); err != nil {
		t.Fatal(err)
	}
	info, _ := file.Stat()
	if _, _, err := (BasicGate{}).Validate(context.Background(), "report.pdf", "", info.Size(), file); err == nil {
		t.Fatal("缺少 EOF 的 PDF 不应通过")
	}
}

func zipFile(t *testing.T, files map[string]string) *os.File {
	t.Helper()
	var payload bytes.Buffer
	archive := zip.NewWriter(&payload)
	for name, content := range files {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(t.TempDir(), "gate-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(payload.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	return file
}
