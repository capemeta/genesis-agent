package gate

import (
	"archive/zip"
	"fmt"
	"io"
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
	if ext == ".pptx" {
		if reason := checkPPTXSlideContent(zr); reason != "" {
			return false, kind, reason
		}
	}
	return true, kind, "ooxml ok"
}

// checkPPTXSlideContent 拒绝空幻灯片（常见于误用 addSlide({title,...}) 而未调用 addText/addTable）。
func checkPPTXSlideContent(zr *zip.ReadCloser) string {
	slideCount := 0
	blankCount := 0
	for _, file := range zr.File {
		name := file.Name
		if !strings.HasPrefix(name, "ppt/slides/slide") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		if strings.Contains(name, "_rels") {
			continue
		}
		slideCount++
		rc, err := file.Open()
		if err != nil {
			return "无法读取幻灯片: " + name + ": " + err.Error()
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return "无法读取幻灯片: " + name + ": " + err.Error()
		}
		if !pptxSlideHasContent(string(data)) {
			blankCount++
		}
	}
	if slideCount == 0 {
		return ""
	}
	if blankCount > 0 {
		return fmt.Sprintf("检测到 %d/%d 张空白幻灯片（无文本/形状）；常见原因是误用 addSlide({title,...}) 而非 let slide=pptx.addSlide(); slide.addText/addTable", blankCount, slideCount)
	}
	return ""
}

func pptxSlideHasContent(xml string) bool {
	if hasNonEmptyDrawingText(xml) {
		return true
	}
	markers := []string{"<p:sp ", "<p:sp>", "<p:pic", "<p:graphicFrame", "<p:cxnSp"}
	for _, m := range markers {
		if strings.Contains(xml, m) {
			return true
		}
	}
	return false
}

func hasNonEmptyDrawingText(xml string) bool {
	const open = "<a:t"
	rest := xml
	for {
		i := strings.Index(rest, open)
		if i < 0 {
			return false
		}
		rest = rest[i+len(open):]
		gt := strings.IndexByte(rest, '>')
		if gt < 0 {
			return false
		}
		rest = rest[gt+1:]
		end := strings.Index(rest, "</a:t>")
		if end < 0 {
			return false
		}
		if strings.TrimSpace(rest[:end]) != "" {
			return true
		}
		rest = rest[end+len("</a:t>"):]
	}
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
