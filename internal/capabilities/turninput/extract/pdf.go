package extract

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// PDF 轻量抽取 PDF 文本层（括号串 / Tj·TJ）；失败由调用方降级为 path-only。
type PDF struct{}

var (
	rePDFLiteral = regexp.MustCompile(`\((?:\\.|[^\\)])*\)`)
	rePDFHex     = regexp.MustCompile(`<[0-9A-Fa-f\s]+>`)
)

func (PDF) CanHandle(path, mime string) bool {
	if strings.ToLower(filepath.Ext(path)) == ".pdf" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(mime), "application/pdf")
}

func (PDF) Extract(path string, maxBytes int) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || !bytes.HasPrefix(raw, []byte("%PDF")) {
		return "", fmt.Errorf("not a pdf")
	}
	text := extractPDFLiterals(raw, maxBytes)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("pdf text layer empty or compressed-only")
	}
	return text, nil
}

func extractPDFLiterals(raw []byte, maxBytes int) string {
	var b strings.Builder
	for _, m := range rePDFLiteral.FindAll(raw, -1) {
		s := unescapePDFString(m)
		s = strings.TrimSpace(s)
		if s == "" || !looksLikeReadable(s) {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(s)
		if b.Len() >= maxBytes {
			break
		}
	}
	if b.Len() < maxBytes/4 {
		for _, m := range rePDFHex.FindAll(raw, -1) {
			s := decodePDFHex(m)
			s = strings.TrimSpace(s)
			if s == "" || !looksLikeReadable(s) {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(s)
			if b.Len() >= maxBytes {
				break
			}
		}
	}
	out := b.String()
	if len(out) > maxBytes {
		out = out[:maxBytes]
	}
	return out
}

func unescapePDFString(m []byte) string {
	if len(m) < 2 {
		return ""
	}
	inner := m[1 : len(m)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			switch inner[i+1] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '(', ')', '\\':
				b.WriteByte(inner[i+1])
			default:
				b.WriteByte(inner[i+1])
			}
			i++
			continue
		}
		b.WriteByte(inner[i])
	}
	return b.String()
}

func decodePDFHex(m []byte) string {
	inner := bytes.ReplaceAll(m[1:len(m)-1], []byte(" "), nil)
	if len(inner)%2 != 0 {
		return ""
	}
	out := make([]byte, 0, len(inner)/2)
	for i := 0; i+1 < len(inner); i += 2 {
		v, err := strconv.ParseUint(string(inner[i:i+2]), 16, 8)
		if err != nil {
			return ""
		}
		out = append(out, byte(v))
	}
	if !utf8.Valid(out) {
		return ""
	}
	return string(out)
}

func looksLikeReadable(s string) bool {
	letters := 0
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r >= 0x4e00 {
			letters++
		}
	}
	return letters >= 2 || (len(s) >= 4 && letters >= 1)
}
