// Package search 提供文件搜索工具共享逻辑。
package search

import (
	"regexp"
	"strings"
)

// GlobMatcher 是面向 workspace slash 路径的简单双星 glob matcher。
type GlobMatcher struct {
	pattern string
	re      *regexp.Regexp
}

// NewGlobMatcher 创建 glob matcher，支持 *, ?, ** 和普通字符转义。
func NewGlobMatcher(pattern string) (*GlobMatcher, error) {
	pattern = strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
	if pattern == "" {
		pattern = "**"
	}
	re, err := regexp.Compile("^" + globToRegexp(pattern) + "$")
	if err != nil {
		return nil, err
	}
	return &GlobMatcher{pattern: pattern, re: re}, nil
}

// Match 判断 path 是否匹配。若 pattern 不含 slash，也允许匹配 basename。
func (m *GlobMatcher) Match(p string) bool {
	p = strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "./")
	if m.re.MatchString(p) {
		return true
	}
	if !strings.Contains(m.pattern, "/") {
		parts := strings.Split(p, "/")
		return m.re.MatchString(parts[len(parts)-1])
	}
	return false
}

func globToRegexp(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	return b.String()
}

// IsProbablyBinary 用 NUL 字节做保守二进制判断。
func IsProbablyBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
