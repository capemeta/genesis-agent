package service

import (
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

func TestPolicyPipeline(t *testing.T) {
	globalConfig := execmodel.SandboxGlobalConfig{
		Local: execmodel.SandboxLocalConfig{
			Enabled:    true,
			Preference: "auto",
		},
		Remote: execmodel.SandboxRemoteConfig{
			Enabled: false, // 默认系统开启/关闭测试
		},
		SkillsOverride: map[string]execmodel.SkillSandboxSpec{
			"office-ppt": {
				PreferredBackend: execmodel.BackendRemoteContainer,
				AllowDegradation: true,
			},
			"strict-tool": {
				PreferredBackend: execmodel.BackendRemoteContainer,
				AllowDegradation: false,
			},
		},
	}

	pipeline := NewPolicyPipeline(globalConfig)

	// 测试用例 1: 系统禁用远程沙箱且 allow_degradation=true ──► 自动降级为本地沙箱并带 Warning
	t.Run("system remote disabled with allow degradation", func(t *testing.T) {
		decision, err := pipeline.Evaluate("office-ppt", nil, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if decision.Target != TargetLocalPlatformDecision {
			t.Errorf("want target local, got %v", decision.Target)
		}
		if !decision.Degraded {
			t.Errorf("want degraded true, got false")
		}
		if len(decision.Warnings) == 0 {
			t.Errorf("expected degradation warnings, got none")
		}
	})

	// 测试用例 2: 系统禁用远程沙箱且 allow_degradation=false ──► Fail Closed 拒绝执行
	t.Run("system remote disabled with fail closed", func(t *testing.T) {
		_, err := pipeline.Evaluate("strict-tool", nil, false)
		if err == nil {
			t.Fatalf("expected fail closed error, got nil")
		}
	})

	// 测试用例 3: 系统开启远程沙箱且远程就绪 ──► 路由到远程沙箱
	t.Run("remote available routed to remote", func(t *testing.T) {
		configWithRemote := globalConfig
		configWithRemote.Remote.Enabled = true
		pipelineRemote := NewPolicyPipeline(configWithRemote)

		decision, err := pipelineRemote.Evaluate("office-ppt", nil, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if decision.Target != TargetRemoteContainerDecision {
			t.Errorf("want target remote, got %v", decision.Target)
		}
		if decision.Degraded {
			t.Errorf("want degraded false, got true")
		}
	})
}
