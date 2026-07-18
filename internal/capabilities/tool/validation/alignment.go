// Package validation 校验 Tool Registry、Profile 与提示词依赖的一致性。
package validation

import (
	"fmt"
	"sort"
	"strings"

	tool "genesis-agent/internal/capabilities/tool/contract"
)

// ValidateEnabled 确保 Profile 中所有精确工具名均已注册。
// 带通配符的动态命名空间由对应能力网关在运行期管理。
func ValidateEnabled(registry tool.Registry, enabled []string) error {
	if registry == nil {
		return fmt.Errorf("tool registry 不能为空")
	}
	missing := make([]string, 0)
	for _, name := range enabled {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, "*?") {
			continue
		}
		if registry.Get(name) == nil {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("Profile 声明了未注册工具: %s", strings.Join(missing, ", "))
}

// PromptToolsAvailable 判断提示词片段引用的工具是否同时启用且已注册。
func PromptToolsAvailable(registry tool.Registry, enabled, required []string) bool {
	set := make(map[string]struct{}, len(enabled))
	for _, name := range enabled {
		set[strings.TrimSpace(name)] = struct{}{}
	}
	for _, name := range required {
		if _, ok := set[name]; !ok || registry == nil || registry.Get(name) == nil {
			return false
		}
	}
	return true
}
