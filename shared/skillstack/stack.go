package skillstack

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

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
	scriptworkspace "genesis-agent/internal/capabilities/skill/script/workspace"
	skillservice "genesis-agent/internal/capabilities/skill/service"
	installskilldeps "genesis-agent/internal/capabilities/skill/tool/install_skill_dependencies"
	listskillresources "genesis-agent/internal/capabilities/skill/tool/list_skill_resources"
	readskillresource "genesis-agent/internal/capabilities/skill/tool/read_skill_resource"
	runskillcommand "genesis-agent/internal/capabilities/skill/tool/run_skill_command"
	searchskillresources "genesis-agent/internal/capabilities/skill/tool/search_skill_resources"
	skilltool "genesis-agent/internal/capabilities/skill/tool/skill"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/platform/logger"
	promptbuilder "genesis-agent/internal/runtime/prompt"
	"genesis-agent/internal/runtime/strategy/react"
)

// ExecStack 是产品执行栈中与 Skill 脚本相关的子集。
type ExecStack struct {
	Runner        execcontract.ExecutionRunner
	Shells        execcontract.ShellCapabilityProvider
	SessionClient sandboxcontract.SessionClient
	FileClient    sandboxcontract.FileSystemClient
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
	SkillExplicitLoader  react.SkillExplicitLoader
	PromptInjector       promptbuilder.ContextInjector
	CatalogRequest       skillcontract.CatalogRequest
}

// BuildEmbedded 仅装配内置 embed Skills + run_skill_command。
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

	scriptSvc, err := scriptservice.New(scriptservice.Deps{
		Skills:                skillSvc,
		Runner:                opts.Exec.Runner,
		Approval:              opts.Approval,
		SessionClient:         opts.Exec.SessionClient,
		FileClient:            opts.Exec.FileClient,
		WorkspaceRef:          opts.Exec.WorkspaceRef,
		Logger:                log,
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
	explicitLoader, ok := skillGateway.(react.SkillExplicitLoader)
	if !ok {
		return nil, fmt.Errorf("Skill 网关未实现显式加载接口")
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
	runSkillCommand, err := runskillcommand.New(runskillcommand.Deps{
		Runner:         scriptSvc,
		CatalogRequest: catalogReq,
		Sandbox:        opts.Exec.Sandbox,
		WorkspaceRoot:  opts.WorkspaceRoot,
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
		var b strings.Builder
		b.WriteString("Skills 是任务流程包，不是可执行工具。加载技能必须调用 Skill(skill=...)；禁止把 office-ppt 等技能名当作独立工具调用。用户输入中的 $skill 或 skill:// 引用会在回合开始自动注入。用户指定的宿主机文件应直接作为 run_skill_command.inputs，由运行时在审批后 stage，禁止 run_command / Copy-Item 手动搬运。可用技能列表见 Skill 工具描述中的 <available_skills>。若 run_skill_command 返回 failure_kind=dependency_missing：调用 install_skill_dependencies（须审批，仅装 runtime 白名单包）后，用相同参数再跑命令（安装成功会清零重复失败计数）；sandbox_violation 勿当成缺包。收到 failure_kind=repeated_failure：禁止再次提交相同调用，必须改参或改策略。收到 failure_kind=no_progress：必须总结阻塞或询问用户，禁止继续空转。")
		b.WriteString("\n\nRun 文件落点：中间脚本/临时文件用 write_file(\"$WORK_DIR/...\")；最终交付进 $OUTPUT_DIR；禁止写到仓库根目录。")
		if req.Run != nil && strings.TrimSpace(req.Run.ID) != "" && strings.TrimSpace(opts.WorkspaceRoot) != "" {
			if ws, err := scriptworkspace.PrepareLocalTask(opts.WorkspaceRoot, req.Run.ID); err == nil {
				rel := func(abs string) string {
					r, err := filepath.Rel(opts.WorkspaceRoot, abs)
					if err != nil {
						return abs
					}
					return filepath.ToSlash(r)
				}
				b.WriteString(fmt.Sprintf("\n本 Run：WORK_DIR=%s OUTPUT_DIR=%s INPUT_DIR=%s", rel(ws.WorkDir), rel(ws.OutputDir), rel(ws.InputDir)))
			}
		}
		return promptbuilder.Fragment{
			Name:     "skills_instructions",
			Contents: b.String(),
		}, nil
	})

	return &Stack{
		Service: skillSvc,
		Tools: []toolcontract.Tool{
			skillGateway, listResources, readResource, searchResources, runSkillCommand, installDeps,
		},
		SkillNameMatcher:     matcher,
		SkillMentionSelector: mentions,
		SkillExplicitLoader:  explicitLoader,
		PromptInjector:       injector,
		CatalogRequest:       catalogReq,
	}, nil
}
