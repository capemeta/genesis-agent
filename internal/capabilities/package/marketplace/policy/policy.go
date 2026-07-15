// Package policy 提供跨产品可复用的 AllowedSourcePolicy 默认实现。
package policy

import (
	"context"
	"fmt"
	"strings"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

// AllowHosts 按域名白名单放行远程安装；本地 dir/file 可由 AllowLocal 控制。
type AllowHosts struct {
	Hosts      []string // 空则默认 github.com
	AllowLocal bool
}

// DefaultAllowHosts 返回 CLI/Desktop 默认策略（仅 github.com + 本地）。
func DefaultAllowHosts() AllowHosts {
	return AllowHosts{Hosts: []string{"github.com"}, AllowLocal: true}
}

// AllowGitHub 兼容旧名，等价于 DefaultAllowHosts。
type AllowGitHub = AllowHosts

func (p AllowHosts) Check(_ context.Context, source marketmodel.MarketplaceSource, _ string) error {
	hosts := normalizeHosts(p.Hosts)
	allowLocal := p.AllowLocal
	if len(p.Hosts) == 0 {
		// 未显式配置 Hosts 时（AllowHosts{} / AllowGitHub{}），本地默认允许
		allowLocal = true
	}
	switch source.Type {
	case marketmodel.SourceTypeDirectory, marketmodel.SourceTypeFile:
		if allowLocal {
			return nil
		}
		return fmt.Errorf("策略禁止本地路径安装")
	case marketmodel.SourceTypeGitHub, marketmodel.SourceTypeGit, marketmodel.SourceTypeURL:
		domain := marketmodel.SourceDomain(source)
		if domain == "" {
			return fmt.Errorf("无法解析来源域名")
		}
		if hostAllowed(domain, hosts) {
			return nil
		}
		return fmt.Errorf("来源域名 %q 不在 skills.install.allowed_hosts 中（当前: %s）", domain, strings.Join(hosts, ", "))
	default:
		return fmt.Errorf("不支持的来源类型: %s", source.Type)
	}
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

// DenyAllRemote Enterprise 默认桩：拒绝远程与本地旁路安装，直至管理面 allowlist。
type DenyAllRemote struct{}

func (DenyAllRemote) Check(_ context.Context, source marketmodel.MarketplaceSource, _ string) error {
	switch source.Type {
	case marketmodel.SourceTypeDirectory, marketmodel.SourceTypeFile:
		return fmt.Errorf("enterprise 默认拒绝本地路径安装；请走管理面 Import")
	default:
		return fmt.Errorf("enterprise 默认拒绝远程 Skill 安装；请配置 allowlist 并走 Import 审核")
	}
}
