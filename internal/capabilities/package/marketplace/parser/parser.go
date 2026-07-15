// Package parser 提供产品无关的远程 Marketplace / Skill 来源解析（无本地 I/O）。
package parser

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

var githubShorthandPattern = regexp.MustCompile(`^[^/\s]+/[^/\s]+$`)

// Options 控制远程解析行为。
type Options struct {
	// GitHosts 视为「GitHub 兼容 forge」的主机（仅识别 owner/repo 与 /tree|/blob）。
	// 空则默认 ["github.com"]。下载站也应列入 allowed_hosts，但带 query 的 API 会走 URL 通道。
	GitHosts []string
}

// ParseRemote 使用默认主机列表（仅 github.com）解析。
func ParseRemote(input string) (marketmodel.MarketplaceSource, error) {
	return ParseRemoteWith(input, Options{})
}

// ParseRemoteWith 按 Options.GitHosts 解析远程来源。
func ParseRemoteWith(input string, opts Options) (marketmodel.MarketplaceSource, error) {
	hosts := normalizeHosts(opts.GitHosts)
	raw := strings.TrimSpace(input)
	if raw == "" {
		return marketmodel.MarketplaceSource{}, fmt.Errorf("marketplace source不能为空")
	}
	for _, prefix := range []string{"github:", "git:", "url:"} {
		if strings.HasPrefix(raw, prefix) {
			rest := strings.TrimSpace(strings.TrimPrefix(raw, prefix))
			switch prefix {
			case "github:":
				base, ref, sub, err := splitRefAndPath(rest)
				if err != nil {
					return marketmodel.MarketplaceSource{}, err
				}
				return marketmodel.MarketplaceSource{
					Type:    marketmodel.SourceTypeGitHub,
					Host:    "github.com",
					Repo:    base,
					Ref:     ref,
					SubPath: sub,
				}, nil
			case "git:":
				base, ref, sub, err := splitRefAndPath(rest)
				if err != nil {
					return marketmodel.MarketplaceSource{}, err
				}
				if host, ownerRepo, ghRef, ghSub, ok := parseGitForgeURL(base, hosts); ok {
					if ref == "" {
						ref = ghRef
					}
					if sub == "" {
						sub = ghSub
					}
					return marketmodel.MarketplaceSource{
						Type:    marketmodel.SourceTypeGitHub,
						Host:    host,
						Repo:    ownerRepo,
						Ref:     ref,
						SubPath: sub,
					}, nil
				}
				return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeGit, URL: base, Ref: ref, SubPath: sub}, nil
			case "url:":
				if host, ownerRepo, ref, sub, ok := parseGitForgeURL(rest, hosts); ok {
					return marketmodel.MarketplaceSource{
						Type:    marketmodel.SourceTypeGitHub,
						Host:    host,
						Repo:    ownerRepo,
						Ref:     ref,
						SubPath: sub,
					}, nil
				}
				return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeURL, URL: rest}, nil
			}
		}
	}

	if host, ownerRepo, ref, sub, ok := parseGitForgeURL(raw, hosts); ok {
		return marketmodel.MarketplaceSource{
			Type:    marketmodel.SourceTypeGitHub,
			Host:    host,
			Repo:    ownerRepo,
			Ref:     ref,
			SubPath: sub,
		}, nil
	}

	base, ref, sub, err := splitRefAndPath(raw)
	if err != nil {
		return marketmodel.MarketplaceSource{}, err
	}
	if githubShorthandPattern.MatchString(base) {
		return marketmodel.MarketplaceSource{
			Type:    marketmodel.SourceTypeGitHub,
			Host:    "github.com",
			Repo:    base,
			Ref:     ref,
			SubPath: sub,
		}, nil
	}
	if parsed, err := url.Parse(raw); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return marketmodel.MarketplaceSource{Type: marketmodel.SourceTypeURL, URL: raw}, nil
	}
	return marketmodel.MarketplaceSource{}, fmt.Errorf("不支持的marketplace source: %s", input)
}

func normalizeHosts(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	seen := map[string]struct{}{}
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		h = strings.TrimPrefix(h, "https://")
		h = strings.TrimPrefix(h, "http://")
		h = strings.Trim(h, "/")
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	if len(out) == 0 {
		return []string{"github.com"}
	}
	return out
}

func hostAllowed(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, a := range allowed {
		if host == a {
			return true
		}
	}
	return false
}

func splitRefAndPath(input string) (base, ref, subPath string, err error) {
	beforeHash, afterHash, hasHash := strings.Cut(input, "#")
	base, ref, _ = strings.Cut(beforeHash, "@")
	if !hasHash {
		afterHash = ""
	}
	subPath, err = normalizeSubPath(afterHash)
	if err != nil {
		return "", "", "", err
	}
	return strings.TrimSpace(base), strings.TrimSpace(ref), subPath, nil
}

func normalizeSubPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimLeft(value, "/\\")
	value = strings.ReplaceAll(value, "\\", "/")
	value = strings.TrimRight(value, "/")
	if value == "." || value == "" {
		return "", nil
	}
	if strings.Contains(value, "..") {
		return "", fmt.Errorf("marketplace path不能包含..: %s", value)
	}
	return value, nil
}

// parseGitForgeURL 解析 GitHub 兼容主机上的「仓库」URL。
// 兼容规则（避免把下载 API 误判为 owner/repo）：
//  1. 带 query 的 URL 一律不当 forge（如 openskills /api/download?slug=...）
//  2. 仅识别：恰好 owner/repo，或 /tree|/blob/<ref>/...
//  3. SSH：git@host:owner/repo
func parseGitForgeURL(input string, hosts []string) (host, repo, ref, subPath string, ok bool) {
	raw := strings.TrimSpace(input)
	for _, h := range hosts {
		prefix := "git@" + h + ":"
		if strings.HasPrefix(raw, prefix) {
			rest := strings.TrimSuffix(strings.TrimPrefix(raw, prefix), ".git")
			parts := strings.Split(strings.Trim(rest, "/"), "/")
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				return h, parts[0] + "/" + parts[1], "", "", true
			}
			return "", "", "", "", false
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", "", "", false
	}
	host = strings.ToLower(parsed.Hostname())
	if !hostAllowed(host, hosts) {
		return "", "", "", "", false
	}
	// 下载站/API 几乎总是带 query；带 query 时走 SourceTypeURL 直接拉包。
	if strings.TrimSpace(parsed.RawQuery) != "" {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", "", false
	}
	repo = parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
	if len(parts) == 2 {
		return host, repo, "", "", true
	}
	if parts[2] == "tree" || parts[2] == "blob" {
		if len(parts) < 4 {
			return "", "", "", "", false
		}
		ref = parts[3]
		pathParts := parts[4:]
		if parts[2] == "blob" && len(pathParts) > 0 && strings.EqualFold(pathParts[len(pathParts)-1], "SKILL.md") {
			pathParts = pathParts[:len(pathParts)-1]
		}
		sub := strings.Join(pathParts, "/")
		normalized, err := normalizeSubPath(sub)
		if err != nil {
			return "", "", "", "", false
		}
		return host, repo, ref, normalized, true
	}
	// 其它路径（如 /api/download、/raw/...）不当 forge，留给 URL 下载通道。
	return "", "", "", "", false
}
