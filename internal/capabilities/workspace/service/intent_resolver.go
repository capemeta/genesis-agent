package service

import (
	"context"
	"regexp"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

// referencedFilePattern 覆盖输入/源文件与交付文件引用（用于经典「依据源文件产出」工作区启发式）。
var referencedFilePattern = regexp.MustCompile(`(?i)(?:^|[\s"'（(])[^\s"'，。；：、（）()]+\.(?:md|markdown|txt|csv|tsv|json|ya?ml|pdf|docx?|xlsx?|pptx?|html?|xml|go|py|js|ts|java|sql)(?:$|[\s"'，。；：、）)])`)

// TaskIntentResolver 是产品无关、确定性的意图解析器。
// 它只根据请求语义选择 workspace discipline，不解析真实路径、也不授予权限。
// 交付门禁不在此处承诺：无显式 DeclaredDeliverable 时，由 Harness 在出现可交付产物证据后建约并 Finalize。
type TaskIntentResolver struct{}

func NewTaskIntentResolver() *TaskIntentResolver { return &TaskIntentResolver{} }

func (r *TaskIntentResolver) ResolveIntent(ctx context.Context, req workcontract.ResolveIntentRequest) (workcontract.ExecutionIntent, error) {
	if err := ctx.Err(); err != nil {
		return workcontract.ExecutionIntent{}, err
	}
	intent := req.Supplied
	intent.HasProject = req.HasProject
	if hasExplicitIntent(intent) {
		return intent, nil
	}

	prompt := strings.ToLower(strings.TrimSpace(req.Prompt))
	switch {
	case isPersistentTask(prompt):
		intent.RequiredMode = execmodel.WorkspaceModeSession
		intent.NeedsPersistentRun = true
	case isProjectModification(prompt):
		intent.RequiredMode = execmodel.WorkspaceModeProject
		intent.ModifyProject = true
	case isBoundedFileProduction(prompt):
		// 仅选择 Task 工作区纪律；不置 ArtifactRequired（避免只读/双模态 Skill 被 NLP 误建门禁）。
		intent.RequiredMode = execmodel.WorkspaceModeTask
		intent.BoundedInputs = true
		intent.BoundedOutputs = true
	}
	return intent, nil
}

func hasExplicitIntent(intent workcontract.ExecutionIntent) bool {
	return intent.ExplicitMode != "" || intent.RequiredMode != "" || intent.ModifyProject ||
		intent.BoundedInputs || intent.BoundedOutputs || intent.NeedsPersistentRun || intent.ArtifactRequired
}

func isPersistentTask(prompt string) bool {
	return containsAny(prompt, "持续监控", "长期运行", "后台运行", "定时执行", "持续跟踪", "long-running", "keep monitoring")
}

func isProjectModification(prompt string) bool {
	if !containsAny(prompt, "修复", "重构", "实现功能", "开发功能", "修改代码", "改代码", "删除旧代码", "编译项目", "运行测试", "fix ", "refactor", "implement ") {
		return false
	}
	return containsAny(prompt, "代码", "项目", "仓库", "模块", "接口", "函数", "测试", ".go", ".py", ".js", ".ts", "code", "repo")
}

// isBoundedFileProduction 识别「依据源文件做文件型产出」类任务，仅影响 workspace mode。
func isBoundedFileProduction(prompt string) bool {
	if !referencedFilePattern.MatchString(prompt) {
		return false
	}
	if !containsAny(prompt, "生成", "创建", "新建", "制作", "导出", "转换", "写一个", "写成", "generate", "create", "export", "convert") {
		return false
	}
	return containsAny(prompt, "ppt", "演示文稿", "幻灯片", "pdf", "word", "docx", "excel", "xlsx", "markdown", "md文档", "报告", "总结文档", "文件")
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
