package skillstack

import (
	"context"
	"fmt"
	"io/fs"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	skillembedded "genesis-agent/internal/capabilities/skill/adapter/embedded"
	skillcollision "genesis-agent/internal/capabilities/skill/collision"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillparser "genesis-agent/internal/capabilities/skill/parser"
	scriptservice "genesis-agent/internal/capabilities/skill/script/service"
	skillservice "genesis-agent/internal/capabilities/skill/service"
	installskilldeps "genesis-agent/internal/capabilities/skill/tool/install_skill_dependencies"
	listskillresources "genesis-agent/internal/capabilities/skill/tool/list_skill_resources"
	readskillresource "genesis-agent/internal/capabilities/skill/tool/read_skill_resource"
	runskillscript "genesis-agent/internal/capabilities/skill/tool/run_skill_script"
	searchskillresources "genesis-agent/internal/capabilities/skill/tool/search_skill_resources"
	skilltool "genesis-agent/internal/capabilities/skill/tool/skill"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/runtime/strategy/react"
	promptbuilder "genesis-agent/internal/runtime/prompt"
)

// ExecStack 是产品执行栈中与 Skill 脚本相关的子集。
type ExecStack struct {
	Runner        execcontract.ExecutionRunner
	SessionClient sandboxcontract.SessionClient
	WorkspaceRef  sandboxcontract.WorkspaceRef
	Sandbox       execmodel.SandboxProfile
}

// Options 描述产品侧 Skill 工具栈装配参数。
type Options struct {
	Product        profilemodel.ChannelType
	Environment    profilemodel.RuntimeEnvironment
	Approval       approvalcontract.Service
	Exec           ExecStack
	Logger         logger.Logger
	EnabledTools   []string
	EnabledSkills  []string
	DisabledSkills []string
	WorkspaceRoot  string // 可选；install_skill_dependencies 的 workspace cwd
	// SharedScriptsFS 可选；默认使用 embedded OfficeCommonScriptsFS。
	SharedScriptsFS fs.FS
	// EnablePreflight / AutoRetryAfterInstall 对齐 CLI skills.* 配置（默认 false）。
	EnablePreflight       bool
	AutoRetryAfterInstall bool
	Installer             scriptservice.DependencyInstaller
}

// Stack 是装配结果。
type Stack struct {
	Service              skillcontract.Service
	Tools                []toolcontract.Tool
	SkillNameMatcher     react.SkillNameMatcher
	SkillMentionSelector react.SkillMentionSelector
	PromptInjector       promptbuilder.ContextInjector
	CatalogRequest       skillcontract.CatalogRequest
}

// BuildEmbedded 仅装配内置 embed Skills + run_skill_script（含 SharedScriptsFS）。
// 适用于 Enterprise 等尚未接入本地 Skill 目录/marketplace 的产品；CLI 可继续用更完整装配。
func BuildEmbedded(opts Options) (*Stack, error) {
	if opts.Approval == nil {
		return nil, fmt.Errorf("approval service未配置")
	}
	if opts.Exec.Runner == nil {
		return nil, fmt.Errorf("execution runner未配置")
	}
	if opts.Product == "" {
		opts.Product = profilemodel.ChannelEnterprise
	}
	if opts.Environment == "" {
		opts.Environment = profilemodel.EnvironmentServer
	}
	log := opts.Logger
	if log == nil {
		log = logger.NewNop()
	}

	systemFS, err := skillembedded.SystemFS()
	if err != nil {
		return nil, fmt.Errorf("加载内置Skills失败: %w", err)
	}
	systemSource, err := skillembedded.NewSource(
		skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: string(opts.Product) + "-system"},
		skillmodel.ScopeSystem,
		systemFS,
		skillparser.New(),
	)
	if err != nil {
		return nil, fmt.Errorf("初始化内置Skill Source失败: %w", err)
	}
	skillSvc := skillservice.New([]skillcontract.Source{systemSource}, skillservice.Options{})

	sharedFS := opts.SharedScriptsFS
	if sharedFS == nil {
		sharedFS, err = skillembedded.OfficeCommonScriptsFS()
		if err != nil {
			return nil, fmt.Errorf("加载共享 office scripts失败: %w", err)
		}
	}
	scriptSvc, err := scriptservice.New(scriptservice.Deps{
		Skills:                skillSvc,
		Runner:                opts.Exec.Runner,
		Approval:              opts.Approval,
		SessionClient:         opts.Exec.SessionClient,
		WorkspaceRef:          opts.Exec.WorkspaceRef,
		Logger:                log,
		SharedScriptsFS:       sharedFS,
		EnablePreflight:       opts.EnablePreflight,
		AutoRetryAfterInstall: opts.AutoRetryAfterInstall,
		Installer:             opts.Installer,
	})
	if err != nil {
		return nil, err
	}

	catalogReq := skillcontract.CatalogRequest{
		Product:        opts.Product,
		Environment:    opts.Environment,
		EnabledSkills:  opts.EnabledSkills,
		DisabledSkills: opts.DisabledSkills,
	}

	skillGateway, err := skilltool.New(skilltool.Deps{
		Service:        skillSvc,
		Approval:       opts.Approval,
		CatalogRequest: catalogReq,
		EnabledTools:   opts.EnabledTools,
	})
	if err != nil {
		return nil, err
	}
	listResources, err := listskillresources.New(listskillresources.Deps{
		Service: skillSvc, Approval: opts.Approval, CatalogRequest: catalogReq,
	})
	if err != nil {
		return nil, err
	}
	readResource, err := readskillresource.New(readskillresource.Deps{
		Service: skillSvc, Approval: opts.Approval, CatalogRequest: catalogReq,
	})
	if err != nil {
		return nil, err
	}
	searchResources, err := searchskillresources.New(searchskillresources.Deps{
		Service: skillSvc, Approval: opts.Approval, CatalogRequest: catalogReq,
	})
	if err != nil {
		return nil, err
	}
	runSkillScript, err := runskillscript.New(runskillscript.Deps{
		Runner:         scriptSvc,
		CatalogRequest: catalogReq,
		Sandbox:        opts.Exec.Sandbox,
	})
	if err != nil {
		return nil, err
	}
	installDeps, err := installskilldeps.New(installskilldeps.Deps{
		Skills:         skillSvc,
		Runner:         opts.Exec.Runner,
		Approval:       opts.Approval,
		CatalogRequest: catalogReq,
		Sandbox:        opts.Exec.Sandbox,
		WorkspaceRoot:  opts.WorkspaceRoot,
	})
	if err != nil {
		return nil, err
	}

	matcher := &skillcollision.Matcher{Service: skillSvc, CatalogRequest: catalogReq}
	mentions := &react.MentionSelector{Service: skillSvc, CatalogRequest: catalogReq}
	injector := promptbuilder.ContextInjectorFunc(func(ctx context.Context, req promptbuilder.BuildRequest) (promptbuilder.Fragment, error) {
		return promptbuilder.Fragment{
			Name: "skills_instructions",
			Contents: "Skills 是任务流程包，不是可执行工具。加载技能必须调用 Skill(skill=...)；禁止把 office-ppt 等技能名当作独立工具调用。用户输入中的 $skill 或 skill:// 引用会在回合开始自动注入。可用技能列表见 Skill 工具描述中的 <available_skills>。若 run_skill_script 返回 failure_kind=dependency_missing：调用 install_skill_dependencies（须审批，仅装 runtime 白名单包）后，用相同参数再跑脚本；sandbox_violation 勿当成缺包。",
		}, nil
	})

	return &Stack{
		Service: skillSvc,
		Tools: []toolcontract.Tool{
			skillGateway, listResources, readResource, searchResources, runSkillScript, installDeps,
		},
		SkillNameMatcher:     matcher,
		SkillMentionSelector: mentions,
		PromptInjector:       injector,
		CatalogRequest:       catalogReq,
	}, nil
}
