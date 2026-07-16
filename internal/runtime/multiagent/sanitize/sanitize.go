// Package sanitize 提供子智能体输入和结果共用的最小敏感文本清洗器。
package sanitize

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Text 是可替换的脱敏端口。实现返回错误时，调用方必须 fail closed。
type Text interface {
	Sanitize(value string) (string, error)
}

// Default 使用保守的凭据模式清洗文本。它不是 DLP 的替代品，企业产品可注入更严格实现。
type Default struct{}

var patterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)[^\s]+`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|token|password|secret)\s*[:=]\s*)[^\s,;]+`),
}

// Sanitize 清洗常见凭据并拒绝非法文本。
func (Default) Sanitize(value string) (string, error) {
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("文本编码非法")
	}
	for _, pattern := range patterns {
		value = pattern.ReplaceAllString(value, "$1[redacted]")
	}
	return value, nil
}
