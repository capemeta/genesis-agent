// Package skillmarket 提供 CLI/Desktop 可复用的本地主机 Skill Marketplace 适配。
package skillmarket

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

var githubShorthandPattern = regexp.MustCompile(`^[^/\s]+/[^/\s]+$`)

// Parser 解析 CLI/Desktop 输入形式，兼容 Kode 的 github:/dir:/file:/url: 前缀。
type Parser struct{}

func NewParser() Parser { return Parser{} }

func (Parser) Parse(input string) (marketmodel.MarketplaceSource, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return marketmodel.MarketplaceSource{}, fmt.Errorf("marketplace source不能为空")
	}
	for _, prefix := range []string{"github:", "git:", "url:", "file:", "dir:"} {
		if strings.HasPrefix(raw, prefix) {
			rest := strings.TrimSpace(strings.TrimPrefix(raw, prefix))
			base, ref, sub := splitRefAndPath(rest)
			switch prefix {
			case "github:":
				return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeGitHub, Repo: base, Ref: ref, SubPath: sub}, nil
			case "git:":
				if repo := githubRepoFromURL(base); repo != "" {
					return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeGitHub, Repo: repo, Ref: ref, SubPath: sub}, nil
				}
				return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeGit, URL: base, Ref: ref, SubPath: sub}, nil
			case "url:":
				return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeURL, URL: rest}, nil
			case "file:":
				return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeFile, Path: rest}, nil
			case "dir:":
				return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeDirectory, Path: rest}, nil
			}
		}
	}
	if stat, err := os.Stat(raw); err == nil {
		abs, _ := filepath.Abs(raw)
		if stat.IsDir() {
			return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeDirectory, Path: abs}, nil
		}
		if !stat.IsDir() {
			return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeFile, Path: abs}, nil
		}
	}
	base, ref, sub := splitRefAndPath(raw)
	if githubShorthandPattern.MatchString(base) {
		return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeGitHub, Repo: base, Ref: ref, SubPath: sub}, nil
	}
	if repo := githubRepoFromURL(base); repo != "" {
		return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeGitHub, Repo: repo, Ref: ref, SubPath: sub}, nil
	}
	if parsed, err := url.Parse(raw); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeURL, URL: raw}, nil
	}
	return marketmodel.MarketplaceSource{}, fmt.Errorf("不支持的marketplace source: %s", input)
}

func splitRefAndPath(input string) (base, ref, subPath string) {
	beforeHash, afterHash, hasHash := strings.Cut(input, "#")
	base, ref, _ = strings.Cut(beforeHash, "@")
	if !hasHash {
		afterHash = ""
	}
	return strings.TrimSpace(base), strings.TrimSpace(ref), normalizeSubPath(afterHash)
}

func normalizeSubPath(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimLeft(value, "/\\")
	value = strings.ReplaceAll(value, "\\", "/")
	value = strings.TrimRight(value, "/")
	if value == "." || value == "" {
		return ""
	}
	return value
}

func githubRepoFromURL(input string) string {
	input = strings.TrimSuffix(strings.TrimSpace(input), ".git")
	if strings.HasPrefix(input, "git@github.com:") {
		return strings.TrimPrefix(input, "git@github.com:")
	}
	if parsed, err := url.Parse(input); err == nil && parsed.Host == "github.com" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
	}
	return ""
}
