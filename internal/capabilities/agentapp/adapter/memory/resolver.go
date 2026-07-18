// Package memory 提供内置 App 和测试使用的并发安全有效配置 resolver。
package memory

import (
	"context"
	"fmt"
	"strings"

	agentappcontract "genesis-agent/internal/capabilities/agentapp/contract"
	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

type Resolver struct {
	defaultID string
	profiles  map[string]agentappmodel.EffectiveProfile
}

func NewResolver(defaultID string, profiles []agentappmodel.EffectiveProfile) (*Resolver, error) {
	defaultID = strings.TrimSpace(defaultID)
	if defaultID == "" {
		return nil, fmt.Errorf("agent app resolver 缺少 default app id")
	}
	items := make(map[string]agentappmodel.EffectiveProfile, len(profiles))
	for _, profile := range profiles {
		if err := profile.Validate(); err != nil {
			return nil, err
		}
		if _, exists := items[profile.ID]; exists {
			return nil, fmt.Errorf("agent app %s 重复", profile.ID)
		}
		items[profile.ID] = cloneProfile(profile)
	}
	if _, ok := items[defaultID]; !ok {
		return nil, fmt.Errorf("default agent app %s 不存在", defaultID)
	}
	return &Resolver{defaultID: defaultID, profiles: items}, nil
}

func (r *Resolver) ResolveEffective(ctx context.Context, req agentappcontract.ResolveRequest) (agentappmodel.EffectiveProfile, error) {
	if err := ctx.Err(); err != nil {
		return agentappmodel.EffectiveProfile{}, err
	}
	id := strings.TrimSpace(req.AppID)
	if id == "" {
		id = r.defaultID
	}
	profile, ok := r.profiles[id]
	if !ok {
		return agentappmodel.EffectiveProfile{}, fmt.Errorf("agent app %s 不存在或在当前作用域不可用", id)
	}
	return cloneProfile(profile), nil
}

func cloneProfile(profile agentappmodel.EffectiveProfile) agentappmodel.EffectiveProfile {
	profile.Workspace.AllowedModes = append([]execmodel.WorkspaceMode(nil), profile.Workspace.AllowedModes...)
	return profile
}
