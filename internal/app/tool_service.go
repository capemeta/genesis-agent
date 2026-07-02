package app

import (
	"genesis-agent/internal/capabilities/tool/contract"
)

// ListTools 返回所有已注册工具的元信息列表
func (s *agentServiceImpl) ListTools() []*tool.Info {
	return s.registry.ListInfos()
}
