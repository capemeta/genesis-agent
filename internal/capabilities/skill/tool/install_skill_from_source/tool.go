package install_skill_from_source

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	marketcontract "genesis-agent/internal/capabilities/package/marketplace/contract"
	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

const toolName = "install_skill_from_source"

// SourceInstaller 由产品 bootstrap 注入（通常为 marketplace.Service）。
type SourceInstaller interface {
	InstallFromSource(ctx context.Context, req marketcontract.InstallFromSourceRequest) marketcontract.InstallFromSourceResult
}

// Deps 是工具依赖。
type Deps struct {
	Installer SourceInstaller
	Approval  approvalcontract.Service
	Product   string
}

// Tool 从 URL / GitHub source 安装 Skill 本体（不是 runtime 依赖包）。
type Tool struct {
	deps Deps
}

type input struct {
	Source    string `json:"source"`
	Scope     string `json:"scope,omitempty"`
	SkillPath string `json:"skill_path,omitempty"`
	Force     bool   `json:"force,omitempty"`
	Reason    string `json:"reason,omitempty"`
	AllowURL  bool   `json:"allow_url,omitempty"`
}

type resultPayload struct {
	OK          bool     `json:"ok"`
	Skills      []string `json:"skills,omitempty"`
	Specs       []string `json:"specs,omitempty"`
	Effective   string   `json:"effective,omitempty"`
	NeedsChoice bool     `json:"needs_choice,omitempty"`
	Candidates  []string `json:"candidates,omitempty"`
	FailureKind string   `json:"failure_kind,omitempty"`
	Message     string   `json:"message,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// New 创建工具。
func New(deps Deps) (tool.Tool, error) {
	if deps.Installer == nil {
		return nil, fmt.Errorf("source installer未配置")
	}
	if deps.Approval == nil {
		return nil, fmt.Errorf("approval service未配置")
	}
	if deps.Product == "" {
		deps.Product = "cli"
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name: toolName,
		Description: "从 GitHub URL 或 marketplace source 安装 Skill 本体到本机/当前产品安装目录。" +
			"与 install_skill_dependencies（装 npm/pip）不同。用户给出技能地址时应调用本工具，禁止用 run_command/curl/git clone 旁路安装。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"source": {
					Type:        "string",
					Description: "GitHub URL、github:owner/repo@ref#path、或 package@marketplace",
				},
				"scope": {
					Type:        "string",
					Description: "user 或 project，默认 user",
					Enum:        []string{"user", "project"},
				},
				"skill_path": {
					Type:        "string",
					Description: "多 Skill 仓库时指定相对路径",
				},
				"force": {
					Type:        "boolean",
					Description: "覆盖已安装同名包",
				},
				"reason": {
					Type:        "string",
					Description: "安装原因（审批展示）",
				},
			},
			Required: []string{"source"},
		},
		Traits: tool.ToolTraits{
			Exposure:        tool.ToolExposureDirect,
			ReadOnly:        false,
			ConcurrencySafe: false,
			NeedsPermission: true,
		},
	}
}

func (t *Tool) Execute(ctx context.Context, args string) (string, error) {
	var in input
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return "", fmt.Errorf("解析install_skill_from_source参数失败: %w", err)
	}
	in.Source = strings.TrimSpace(in.Source)
	if in.Source == "" {
		return marshalResult(resultPayload{OK: false, FailureKind: "validation_failed", Error: "source不能为空"})
	}
	scope := marketmodel.InstallScope(strings.TrimSpace(in.Scope))
	if scope == "" {
		scope = marketmodel.InstallScopeUser
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = "安装远程 Skill"
	}
	decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{
		ToolName: toolName,
		Action:   approvalmodel.ActionSkillInstall,
		Resource: approvalmodel.Resource{
			Type:    "skill_source",
			URI:     in.Source,
			Display: in.Source,
			Metadata: map[string]string{
				"source":     in.Source,
				"skill_path": in.SkillPath,
				"scope":      string(scope),
			},
		},
		Reason: reason,
		Risk:   approvalmodel.RiskHigh,
		SuggestedScopes: []approvalmodel.GrantScope{
			approvalmodel.GrantScopeOnce,
			approvalmodel.GrantScopeSession,
		},
	})
	if err != nil {
		return "", err
	}
	if decision.Type == approvalmodel.DecisionDenied || decision.Type == approvalmodel.DecisionAbort || decision.Type == approvalmodel.DecisionTimedOut {
		return marshalResult(resultPayload{
			OK:          false,
			FailureKind: "approval_denied",
			Error:       firstNonEmpty(decision.Reason, "用户拒绝安装 Skill"),
		})
	}

	result := t.deps.Installer.InstallFromSource(ctx, marketcontract.InstallFromSourceRequest{
		SourceInput: in.Source,
		Scope:       scope,
		Force:       in.Force,
		SkillPath:   in.SkillPath,
		AllowURL:    in.AllowURL,
		Product:     t.deps.Product,
	})
	if result.FailureKind != "" || result.NeedsChoice {
		return marshalResult(resultPayload{
			OK:          false,
			FailureKind: result.FailureKind,
			NeedsChoice: result.NeedsChoice,
			Candidates:  result.Candidates,
			Message:     result.Message,
			Error:       result.Message,
		})
	}
	msg := result.Message
	if result.Effective == "next_turn" {
		msg = strings.TrimSpace(msg + "；技能将在下一回合可用（Catalog 未热刷新）")
	}
	return marshalResult(resultPayload{
		OK:        true,
		Skills:    result.Skills,
		Specs:     result.Specs,
		Effective: result.Effective,
		Message:   msg,
	})
}

func marshalResult(payload resultPayload) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
