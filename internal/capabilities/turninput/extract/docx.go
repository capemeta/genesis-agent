package extract

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// Docx 抽取 OOXML word/document.xml 中的纯文本。
type Docx struct{}

func (Docx) CanHandle(path, mime string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".docx" {
		return true
	}
	m := strings.ToLower(mime)
	return strings.Contains(m, "wordprocessingml") || strings.Contains(m, "officedocument.wordprocessingml")
}

func (Docx) Extract(path string, maxBytes int) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open docx: %w", err)
	}
	defer zr.Close()
	var doc io.ReadCloser
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			doc = rc
			break
		}
	}
	if doc == nil {
		return "", fmt.Errorf("docx missing word/document.xml")
	}
	defer doc.Close()
	raw, err := io.ReadAll(io.LimitReader(doc, int64(maxBytes)*4+1))
	if err != nil {
		return "", err
	}
	text := stripOOXMLText(raw)
	if len(text) > maxBytes {
		text = text[:maxBytes]
	}
	return text, nil
}

func stripOOXMLText(raw []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var b strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" || t.Name.Local == "instrText" {
				inText = true
			}
		case xml.EndElement:
			if t.Name.Local == "t" || t.Name.Local == "instrText" {
				inText = false
			}
		case xml.CharData:
			if !inText {
				continue
			}
			s := strings.TrimSpace(string(t))
			if s == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(s)
		}
	}
	return strings.TrimSpace(b.String())
}
