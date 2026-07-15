package service

import (
	"fmt"
	"strings"
	"time"

	"genesis-agent/internal/capabilities/hook/model"
)

// ConfigSource 是已经解析完成的 Hook 配置层。文件读取属于平台配置层，避免 Hook 域依赖本机文件系统。
type ConfigSource struct {
	Name    string
	Managed bool
	Config  model.Config
}

func ApplyDefaults(cfg *model.Config, product string) {
	if cfg.Enabled == nil {
		enabled := true
		cfg.Enabled = &enabled
	}
	if cfg.Execution == "" {
		cfg.Execution = "host"
	}
	if product == "enterprise" {
		cfg.Execution = "sandbox"
		cfg.AllowManagedOnly = true
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 30 * time.Second
	}
	if cfg.Events == nil {
		cfg.Events = map[model.EventName][]model.HookSpec{}
	}
	if cfg.State == nil {
		cfg.State = map[string]model.HookState{}
	}
	guards := model.HookSpec{Matcher: "*", Handlers: []model.HandlerSpec{{Name: "git_branch_guard", Type: "builtin", Builtin: "git_branch_guard"}, {Name: "secret_path_guard", Type: "builtin", Builtin: "secret_path_guard"}}}
	cfg.Events[model.EventPreToolUse] = append([]model.HookSpec{guards}, cfg.Events[model.EventPreToolUse]...)
}

func ValidateConfig(cfg model.Config) error {
	if cfg.Execution != "host" && cfg.Execution != "sandbox" {
		return fmt.Errorf("hooks.execution 必须是 host 或 sandbox")
	}
	return nil
}

// MergeConfigSources 按低到高优先级合并 Hook 配置。事件组追加，state 按稳定 key 就近覆盖。
// allowManagedOnly 生效时，仅保留 managed 来源（builtin 守卫由平台默认层提供）。
func MergeConfigSources(sources []ConfigSource) model.Config {
	result := model.Config{Events: map[model.EventName][]model.HookSpec{}, State: map[string]model.HookState{}}
	for _, source := range sources {
		cfg := source.Config
		if cfg.Enabled != nil {
			result.Enabled = cfg.Enabled
		}
		if strings.TrimSpace(cfg.Execution) != "" {
			result.Execution = cfg.Execution
		}
		if cfg.DefaultTimeout > 0 {
			result.DefaultTimeout = cfg.DefaultTimeout
		}
		if cfg.AllowManagedOnly {
			result.AllowManagedOnly = true
		}
		for event, groups := range cfg.Events {
			for _, group := range groups {
				group.Managed = group.Managed || source.Managed
				for index := range group.Handlers {
					group.Handlers[index].Managed = group.Handlers[index].Managed || source.Managed
				}
				result.Events[event] = append(result.Events[event], group)
			}
		}
		for key, state := range cfg.State {
			result.State[key] = state
		}
	}
	if result.AllowManagedOnly {
		for event, groups := range result.Events {
			filtered := groups[:0]
			for _, group := range groups {
				if group.Managed || groupHasBuiltin(group) {
					filtered = append(filtered, group)
				}
			}
			result.Events[event] = filtered
		}
	}
	return result
}
