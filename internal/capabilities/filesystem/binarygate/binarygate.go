package binarygate

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
)

// ContentClass 描述文件内容是否适合通过文本工具直接返回。
type ContentClass struct {
	Binary bool
	MIME   string
}

// RejectFakeOfficeBinary 阻止用纯文本冒充 OOXML/PDF 交付物。
func RejectFakeOfficeBinary(path string, content []byte) error {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".pptx", ".docx", ".xlsx":
		if len(content) < 4 || content[0] != 'P' || content[1] != 'K' {
			return fscontract.NewError(fscontract.ErrCodeInvalidInput, path, fmt.Errorf("禁止用纯文本冒充 %s；请通过 run_skill_command 生成合法 OOXML", ext))
		}
	case ".pdf":
		if len(content) < 5 || string(content[:5]) != "%PDF-" {
			return fscontract.NewError(fscontract.ErrCodeInvalidInput, path, fmt.Errorf("禁止用纯文本冒充 .pdf；请通过 run_skill_command 或转换脚本生成"))
		}
	}
	return nil
}

// ClassifyContent 判断文件内容是否应按二进制处理。
// 规则先按稳定文件类型识别，再按采样字节兜底，供 read_file、日志预览和未来媒体工具复用。
func ClassifyContent(path string, data []byte) ContentClass {
	ext := strings.ToLower(filepath.Ext(path))
	if mime, ok := binaryMIMEByExt[ext]; ok {
		return ContentClass{Binary: true, MIME: mime}
	}
	sample := data
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	if bytes.IndexByte(sample, 0) >= 0 || !utf8.Valid(sample) {
		return ContentClass{Binary: true, MIME: "application/octet-stream"}
	}
	return ContentClass{}
}

var binaryMIMEByExt = map[string]string{
	".pptx":  "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".docx":  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx":  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pdf":   "application/pdf",
	".zip":   "application/zip",
	".7z":    "application/x-7z-compressed",
	".gz":    "application/gzip",
	".tar":   "application/x-tar",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".mp3":   "audio/mpeg",
	".wav":   "audio/wav",
	".mp4":   "video/mp4",
	".mov":   "video/quicktime",
	".avi":   "video/x-msvideo",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".exe":   "application/vnd.microsoft.portable-executable",
	".dll":   "application/vnd.microsoft.portable-executable",
}

