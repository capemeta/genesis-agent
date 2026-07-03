// Package sandbox 放 CLI/Desktop 可复用的本机平台沙箱选择与计划构造。
package sandbox

import "context"

// Manager 负责探测和选择本机平台沙箱。
type Manager struct {
	backend platformBackend
}

// NewManager 创建本机沙箱 manager。
func NewManager() *Manager { return &Manager{backend: defaultPlatformBackend()} }

// NewManagerWithBackend 创建指定 backend 的 manager，主要用于测试和产品定制。
func NewManagerWithBackend(backend platformBackend) *Manager {
	if backend == nil {
		backend = unavailableBackend{reason: "platform sandbox backend未配置"}
	}
	return &Manager{backend: backend}
}

// Detect 返回当前平台沙箱能力。
func (m *Manager) Detect(ctx context.Context) ([]Capability, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.backend.Detect(ctx)
}

// BuildPlan 根据偏好、策略和当前平台能力构造 sandbox plan。
func (m *Manager) BuildPlan(ctx context.Context, req BuildRequest) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, NewError(ErrCodeInvalidInput, err)
	}
	req = req.withDefaults()
	if req.Preference == PreferenceDisabled {
		return directPlan(req, "sandbox disabled by preference"), nil
	}
	if !req.Profile.RequiresPlatformSandbox() && req.Preference != PreferenceRequired {
		return directPlan(req, "sandbox not required by effective policy"), nil
	}
	plan, err := m.backend.BuildPlan(ctx, req)
	if err == nil {
		if plan == nil {
			return nil, NewError(ErrCodeSandboxInitFailed, nil).WithReason("platform backend returned nil plan")
		}
		plan.CompleteAuditTags(req.Preference)
		return plan, nil
	}
	if req.Preference == PreferenceRequired {
		return nil, err
	}
	if IsUnavailable(err) || IsPolicyUnsupported(err) {
		plan := directPlan(req, err.Error())
		plan.Degraded = true
		plan.Warnings = append(plan.Warnings, "平台沙箱不可用或不支持当前策略，已按auto偏好降级为无平台沙箱: "+err.Error())
		plan.UnsupportedReasons = append(plan.UnsupportedReasons, err.Error())
		plan.CompleteAuditTags(req.Preference)
		return plan, nil
	}
	return nil, err
}

func directPlan(req BuildRequest, reason string) *Plan {
	warnings := []string(nil)
	if reason != "" {
		warnings = append(warnings, reason)
	}
	plan := &Plan{
		Type:                    TypeNone,
		Enforcement:             EnforcementNone,
		Command:                 req.Command.Clone(),
		FileSystemPolicy:        req.Profile.FileSystem,
		NetworkPolicy:           req.Profile.Network,
		ProcessPolicy:           req.Profile.Process,
		Warnings:                warnings,
		EffectiveSandboxProfile: req.Profile,
	}
	plan.CompleteAuditTags(req.Preference)
	return plan
}

type platformBackend interface {
	Detect(ctx context.Context) ([]Capability, error)
	BuildPlan(ctx context.Context, req BuildRequest) (*Plan, error)
}

type unavailableBackend struct{ reason string }

func (b unavailableBackend) Detect(ctx context.Context) ([]Capability, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reason := b.reason
	if reason == "" {
		reason = "platform sandbox unavailable"
	}
	return []Capability{{Type: TypeNone, Available: false, Reason: reason}}, nil
}

func (b unavailableBackend) BuildPlan(ctx context.Context, req BuildRequest) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reason := b.reason
	if reason == "" {
		reason = "platform sandbox unavailable"
	}
	return nil, NewError(ErrCodeSandboxUnavailable, nil).WithReason(reason)
}
