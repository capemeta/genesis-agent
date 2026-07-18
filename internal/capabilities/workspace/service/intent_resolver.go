package service

import (
	"context"
	"regexp"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

// referencedFilePattern 覆盖输入/源文件与交付文件引用（用于经典「依据源文件产出」启发式）。
var referencedFilePattern = regexp.MustCompile(`(?i)(?:^|[\s"'（(])[^\s"'，。；：、（）()]+\.(?:md|markdown|txt|csv|tsv|json|ya?ml|pdf|docx?|xlsx?|pptx?|html?|xml|go|py|js|ts|java|sql)(?:$|[\s"'，。；：、）)])`)

// deliverableFilenamePattern 匹配用户可见交付类目标名（含「重命名为aaa.pptx」中的 aaa.pptx，
// 以及「2026笔记本选型比较.pptx」这类本地化名）。与 artifact initializer 提取策略对齐。
var deliverableFilenamePattern = regexp.MustCompile(`(?i)(?:[A-Za-z0-9][A-Za-z0-9._-]*|[0-9\p{Han}][\p{Han}A-Za-z0-9._-]*)\.(?:pptx|docx|xlsx|pdf)`)

// TaskIntentResolver 是产品无关、确定性的意图解析器。
// 它只根据请求语义选择 workspace discipline 和完成门禁，不解析真实路径、也不授予权限。
// 交付落盘由 Harness（DeliverableSpec → FinalizeRequired）稳定执行；此处只决定是否承诺交付契约。
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
	case commitsToUserVisibleDeliverable(prompt):
		intent.RequiredMode = execmodel.WorkspaceModeTask
		intent.BoundedInputs = true
		intent.BoundedOutputs = true
		intent.ArtifactRequired = true
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

// commitsToUserVisibleDeliverable 判断本轮是否承诺变更用户可见交付文件。
// 优先：Prompt 显式给出交付类目标文件名（改名/另存/修改/生成等均适用）→ 交由 Harness 建 Spec 并 Delivery。
// 其次：经典「依据源文件 + 产出动词 + 类型词」启发式（无目标文件名时仍可建契约）。
func commitsToUserVisibleDeliverable(prompt string) bool {
	if isDeliverableFilenameCommitment(prompt) {
		return true
	}
	return isBoundedFileProduction(prompt)
}

func isDeliverableFilenameCommitment(prompt string) bool {
	if !deliverableFilenamePattern.MatchString(prompt) {
		return false
	}
	// 纯查阅不建交付门禁，避免「打开 xxx.pptx 看看」被 incomplete 卡住。
	if isReadOnlyDeliverableMention(prompt) {
		return false
	}
	return true
}

func isReadOnlyDeliverableMention(prompt string) bool {
	return containsAny(prompt,
		"打开看看", "看一下", "看看内容", "查看内容", "阅读一下", "只读",
		"open and read", "just read", "take a look", "inspect the",
	) || (containsAny(prompt, "打开", "查看", "阅读", "read ", "open ", "inspect ") &&
		!containsAny(prompt, "改", "修改", "重命名", "改名", "另存", "拷贝", "复制", "生成", "创建", "制作", "导出", "写成", "保存",
			"rename", "copy", "save", "create", "generate", "export", "update", "edit", "modify", "change"))
}

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
