// Package extract 提供可插拔文档轻量抽取（预算内、可失败降级）。
package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultMaxBytes = 32 * 1024

// Extractor 从本地路径抽取纯文本。
type Extractor interface {
	// CanHandle 按扩展名 / MIME 判断。
	CanHandle(path, mime string) bool
	Extract(path string, maxBytes int) (string, error)
}

// Registry 按顺序尝试 Extractor。
type Registry struct {
	items []Extractor
}

// DefaultRegistry 返回内置 plaintext + docx + pdf。
func DefaultRegistry() *Registry {
	return &Registry{items: []Extractor{PlainText{}, Docx{}, PDF{}}}
}

// Extract 尝试抽取；无匹配或失败返回 ("", err)。
func (r *Registry) Extract(path, mime string, maxBytes int) (string, error) {
	if r == nil {
		return "", fmt.Errorf("extract registry nil")
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	for _, item := range r.items {
		if item == nil || !item.CanHandle(path, mime) {
			continue
		}
		return item.Extract(path, maxBytes)
	}
	return "", fmt.Errorf("no extractor for %s", filepath.Base(path))
}

// PlainText 处理 txt/md/csv/json/yaml 与 text/*。
type PlainText struct{}

func (PlainText) CanHandle(path, mime string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".csv", ".json", ".yaml", ".yml", ".log":
		return true
	}
	return strings.HasPrefix(strings.ToLower(mime), "text/")
}

func (PlainText) Extract(path string, maxBytes int) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(raw) > maxBytes {
		raw = raw[:maxBytes]
	}
	return string(raw), nil
}
