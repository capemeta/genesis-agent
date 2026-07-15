// Package skill 提供 Enterprise 侧 Skill 安装治理桩（P0）。
package skill

import marketpolicy "genesis-agent/internal/capabilities/package/marketplace/policy"

// DefaultAllowedSourcePolicy 返回 Enterprise 默认拒绝远程安装的策略桩。
// P2 将替换为基于 DB allowlist 的实现；对话工具默认不注册。
func DefaultAllowedSourcePolicy() marketpolicy.DenyAllRemote {
	return marketpolicy.DenyAllRemote{}
}
