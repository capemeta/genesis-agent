package service

import (
	"sort"
	"strings"

	"genesis-agent/internal/capabilities/hook/model"
)

// EffectiveHandler 是在给定产品与运行范围下实际可参与调度的 Hook handler 摘要。
// 它只描述配置计算结果，不泄露 command 内容，适合 CLI、Desktop 和 Enterprise 的管理面复用。
type EffectiveHandler struct {
	Event        model.EventName `json:"event"`
	Matcher      string          `json:"matcher"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	Builtin      string          `json:"builtin,omitempty"`
	Key          string          `json:"key"`
	Managed      bool            `json:"managed"`
	Trusted      bool            `json:"trusted"`
	GroupScope   model.Scope     `json:"group_scope"`
	HandlerScope model.Scope     `json:"handler_scope"`
}

// ListEffectiveHandlers 返回指定运行范围下已经启用、且满足治理策略的 handlers。
// MatchKey 属于一次实际事件的运行时事实，故此处保留 matcher 原文而不进行 matcher 过滤。
func ListEffectiveHandlers(config model.Config, scope model.ScopeContext) []EffectiveHandler {
	if !config.IsEnabled() {
		return nil
	}
	events := make([]string, 0, len(config.Events))
	for event := range config.Events {
		events = append(events, string(event))
	}
	sort.Strings(events)

	items := make([]EffectiveHandler, 0)
	for _, name := range events {
		event := model.EventName(name)
		for _, group := range config.Events[event] {
			if !group.IsEnabled() || !scopeMatches(group.Scope, scope) || (config.AllowManagedOnly && !group.Managed && !groupHasBuiltin(group)) {
				continue
			}
			for _, spec := range group.Handlers {
				state := config.State[HandlerKey(event, group.Matcher, spec)]
				if !effectiveEnabled(group, spec, state) || !scopeMatches(spec.Scope, scope) || (config.AllowManagedOnly && !strings.EqualFold(spec.Type, "builtin") && !group.Managed && !spec.Managed) {
					continue
				}
				items = append(items, EffectiveHandler{
					Event:        event,
					Matcher:      group.Matcher,
					Name:         spec.Name,
					Type:         spec.Type,
					Builtin:      spec.Builtin,
					Key:          HandlerKey(event, group.Matcher, spec),
					Managed:      group.Managed || spec.Managed,
					Trusted:      !strings.EqualFold(spec.Type, "command") || trusted(spec, state),
					GroupScope:   group.Scope,
					HandlerScope: spec.Scope,
				})
			}
		}
	}
	return items
}
