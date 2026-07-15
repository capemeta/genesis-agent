// Package skillmarket 提供 CLI/Desktop 可复用的本地主机 Skill Marketplace 适配。
package skillmarket

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	marketparser "genesis-agent/internal/capabilities/package/marketplace/parser"
	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

// Parser 解析 CLI/Desktop 输入：本地 dir/file + 远程语义（委托 internal parser）。
type Parser struct {
	GitHosts []string
}

func NewParser() Parser {
	return Parser{GitHosts: []string{"github.com"}}
}

// NewParserWithHosts 使用配置的 Git 兼容主机列表。
func NewParserWithHosts(hosts []string) Parser {
	if len(hosts) == 0 {
		return NewParser()
	}
	return Parser{GitHosts: append([]string(nil), hosts...)}
}

func (p Parser) Parse(input string) (marketmodel.MarketplaceSource, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return marketmodel.MarketplaceSource{}, fmt.Errorf("marketplace source不能为空")
	}
	for _, prefix := range []string{"file:", "dir:"} {
		if strings.HasPrefix(raw, prefix) {
			rest := strings.TrimSpace(strings.TrimPrefix(raw, prefix))
			switch prefix {
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
		return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeFile, Path: abs}, nil
	}
	return marketparser.ParseRemoteWith(raw, marketparser.Options{GitHosts: p.GitHosts})
}
