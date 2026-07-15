package skillmarket

import marketpolicy "genesis-agent/internal/capabilities/package/marketplace/policy"

// 兼容别名：CLI/Desktop 既有引用可继续使用。
type AllowGitHubPolicy = marketpolicy.AllowGitHub
type DenyAllRemotePolicy = marketpolicy.DenyAllRemote
