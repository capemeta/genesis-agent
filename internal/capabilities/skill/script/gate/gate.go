package gate

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CheckDelivery 对已知交付扩展名做轻量格式门禁。
func CheckDelivery(path string) (ok bool, kind string, reason string) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".pptx", ".docx", ".xlsx":
		return checkOOXML(path, ext)
	case ".pdf":
		return checkPDF(path)
	default:
		info, err := os.Stat(path)
		if err != nil {
			return false, "file", err.Error()
		}
		return true, "file", fmt.Sprintf("size=%d", info.Size())
	}
}

func checkOOXML(path, ext string) (bool, string, string) {
	kind := strings.TrimPrefix(ext, ".")
	f, err := os.Open(path)
	if err != nil {
		return false, kind, err.Error()
	}
	defer f.Close()
	var header [4]byte
	if _, err := f.Read(header[:]); err != nil {
		return false, kind, "无法读取文件头"
	}
	if header[0] != 'P' || header[1] != 'K' {
		return false, kind, "不是合法 OOXML（缺少 ZIP/PK 魔数）；禁止用纯文本冒充 " + ext
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, kind, "不是合法 ZIP/OOXML: " + err.Error()
	}
	defer zr.Close()
	hasContentTypes := false
	hasOfficePart := false
	need := map[string]string{
		".pptx": "ppt/",
		".docx": "word/",
		".xlsx": "xl/",
	}[ext]
	for _, file := range zr.File {
		name := file.Name
		if name == "[Content_Types].xml" {
			hasContentTypes = true
		}
		if need != "" && strings.HasPrefix(name, need) {
			hasOfficePart = true
		}
	}
	if !hasContentTypes {
		return false, kind, "缺少 [Content_Types].xml"
	}
	if need != "" && !hasOfficePart {
		return false, kind, "缺少 Office 部件前缀 " + need
	}
	return true, kind, "ooxml ok"
}

func checkPDF(path string) (bool, string, string) {
	f, err := os.Open(path)
	if err != nil {
		return false, "pdf", err.Error()
	}
	defer f.Close()
	var header [5]byte
	if _, err := f.Read(header[:]); err != nil {
		return false, "pdf", "无法读取文件头"
	}
	if string(header[:]) != "%PDF-" {
		return false, "pdf", "不是合法 PDF（缺少 %PDF- 魔数）"
	}
	return true, "pdf", "pdf ok"
}
