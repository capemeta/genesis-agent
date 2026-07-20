package service

import (
	"fmt"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// DecisionTarget 策略判定后的目标执行后端。
type DecisionTarget string

const (
	TargetLocalPlatformDecision DecisionTarget = "local_platform_sandbox"
	TargetRemoteContainerDecision DecisionTarget = "remote_sandbox"
	TargetDeniedDecision DecisionTarget = "denied"
)

// PolicyDecision 四层策略评估器返回的决策结果。
type PolicyDecision struct {
	Target           DecisionTarget           `json:"target"`
	ExecutionMode    execmodel.ExecutionMode  `json:"execution_mode"`
	Degraded         bool                     `json:"degraded"`
	Warnings         []string                 `json:"warnings"`
	EffectiveSpec    execmodel.SkillSandboxSpec `json:"effective_spec"`
}

// PolicyPipeline 实现 Layer 1 到 Layer 4 的四层链式策略评估逻辑。
type PolicyPipeline struct {
	globalConfig execmodel.SandboxGlobalConfig
}

// NewPolicyPipeline 创建四层策略评估器。
func NewPolicyPipeline(globalConfig execmodel.SandboxGlobalConfig) *PolicyPipeline {
	return &PolicyPipeline{
		globalConfig: globalConfig,
	}
}

// Evaluate 综合系统全局配置、Skill内置Spec、外部覆写配置与远程可用性做出终极路由决策。
func (p *PolicyPipeline) Evaluate(skillName string, rawSpec *execmodel.SkillSandboxSpec, remoteAvailable bool) (PolicyDecision, error) {
	decision := PolicyDecision{
		Target:        TargetLocalPlatformDecision,
		ExecutionMode: execmodel.ExecutionModePerCall,
		Degraded:      false,
		Warnings:      make([]string, 0),
	}

	// 1. 基础 Spec 合并: 外部 overrides > SKILL.md Frontmatter > 系统缺省
	effectiveSpec := execmodel.DefaultSkillSandboxSpec()
	if rawSpec != nil {
		effectiveSpec = *rawSpec
	}

	if override, exists := p.globalConfig.SkillsOverride[skillName]; exists {
		if override.ExecutionMode != "" {
			effectiveSpec.ExecutionMode = override.ExecutionMode
		}
		if override.PreferredBackend != "" {
			effectiveSpec.PreferredBackend = override.PreferredBackend
		}
		if override.TrustLevel != "" {
			effectiveSpec.TrustLevel = override.TrustLevel
		}
		effectiveSpec.AllowDegradation = override.AllowDegradation
	}

	decision.ExecutionMode = effectiveSpec.ExecutionMode
	decision.EffectiveSpec = effectiveSpec

	// 第三方未审核 Skill 强隔离逻辑: 默认倾向远程沙箱
	if effectiveSpec.TrustLevel == execmodel.TrustLevelUntrusted && effectiveSpec.PreferredBackend == "" {
		effectiveSpec.PreferredBackend = execmodel.BackendRemoteContainer
	}

	// 2. Layer 1 检查: 系统顶层硬门控
	remoteRequested := effectiveSpec.PreferredBackend == execmodel.BackendRemoteContainer
	systemRemoteEnabled := p.globalConfig.Remote.Enabled

	if remoteRequested && !systemRemoteEnabled {
		if !effectiveSpec.AllowDegradation {
			return decision, fmt.Errorf("系统全局配置禁用远程沙箱 (remote.enabled=false)，且 Skill %q 不允许降级 (allow_degradation=false): 拒绝执行", skillName)
		}
		// 允许优雅降级为本地沙箱
		decision.Target = TargetLocalPlatformDecision
		decision.Degraded = true
		decision.Warnings = append(decision.Warnings, fmt.Sprintf("系统未开启远程沙箱，Skill %q 优雅降级为本地 OS 平台沙箱执行", skillName))
		return decision, nil
	}

	// 3. Layer 3 降级检查: 远程请求与后端健康状态
	if remoteRequested && systemRemoteEnabled {
		if remoteAvailable {
			decision.Target = TargetRemoteContainerDecision
			return decision, nil
		}

		// 远程服务不可用但已开启
		if !effectiveSpec.AllowDegradation {
			return decision, fmt.Errorf("远程沙箱后端不可用，且 Skill %q 禁止降级 (allow_degradation=false): 拒绝执行", skillName)
		}

		// 通用降级法则: 自动优雅降级为本地 OS 沙箱
		decision.Target = TargetLocalPlatformDecision
		decision.Degraded = true
		decision.Warnings = append(decision.Warnings, fmt.Sprintf("远程沙箱服务不可用，Skill %q 根据通用降级法则优雅降级为本地 OS 沙箱执行", skillName))
		return decision, nil
	}

	// 4. 默认本地 OS 沙箱执行
	decision.Target = TargetLocalPlatformDecision
	return decision, nil
}
