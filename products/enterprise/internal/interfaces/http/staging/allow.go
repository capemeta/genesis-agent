package staging

import (
	"path/filepath"
	"strings"
)

// IsImageMIME 判断是否为 LLM 当前可视觉消费的图片（StartRun content_base64 仅允许此类）。
func IsImageMIME(mime, name string) bool {
	m := strings.ToLower(strings.TrimSpace(mime))
	switch m {
	case "image/jpeg", "image/jpg", "image/png", "image/webp", "image/gif":
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	}
	return false
}

// IsAllowedUpload 判断 POST /files 是否接受该类型（常用文档/图/音视频/压缩包；拒绝任意二进制）。
func IsAllowedUpload(mime, name string) bool {
	if IsImageMIME(mime, name) {
		return true
	}
	m := strings.ToLower(strings.TrimSpace(mime))
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".txt", ".md", ".csv", ".json", ".yaml", ".yml", ".log",
		".mp4", ".webm", ".mov", ".mkv",
		".mp3", ".wav", ".m4a", ".ogg", ".flac",
		".zip", ".tar", ".gz", ".tgz", ".7z", ".rar":
		return true
	}
	switch {
	case strings.HasPrefix(m, "text/"):
		return true
	case m == "application/pdf":
		return true
	case strings.Contains(m, "officedocument"), strings.Contains(m, "msword"),
		strings.Contains(m, "ms-excel"), strings.Contains(m, "ms-powerpoint"):
		return true
	case strings.HasPrefix(m, "video/"), strings.HasPrefix(m, "audio/"):
		return true
	case m == "application/zip", m == "application/x-zip-compressed",
		m == "application/x-tar", m == "application/gzip", m == "application/x-7z-compressed",
		m == "application/x-rar-compressed", m == "application/vnd.rar":
		return true
	case strings.HasPrefix(m, "image/"):
		// 其它 image/*（如 bmp/tiff）上传允许；StartRun 内联 base64 仍只限 jpeg/png/webp/gif
		return true
	}
	return false
}
