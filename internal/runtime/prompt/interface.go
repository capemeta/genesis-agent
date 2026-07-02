// Package prompt 定义运行时提示词构建接口与默认实现。
package prompt

import "genesis-agent/internal/domain"

// Builder 构建运行时提示词。
type Builder interface {
	BuildSystem(agent *domain.Agent) string
}
