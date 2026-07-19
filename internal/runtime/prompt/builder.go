// Package prompt 负责构建发送给 LLM 的系统提示词
// 支持静态提示词和动态注入（记忆、技能等）
package prompt

import (
	"context"
	"fmt"
	"strings"
	"time"

	subagentprompt "genesis-agent/internal/capabilities/subagent/prompt"
	planmodeprompt "genesis-agent/internal/capabilities/planmode/prompt"
	tasklistprompt "genesis-agent/internal/capabilities/tasklist/prompt"
	"genesis-agent/internal/runtime/collab"
)

// DefaultBuilder 系统提示词构建器。
type DefaultBuilder struct {
	injectors         []ContextInjector
	delegationPosture subagentprompt.Posture
}

// Option 配置提示词构建器。
type Option func(*DefaultBuilder)

// WithDelegationPosture 设置产品默认委派姿态（请求级字段可覆盖）。
func WithDelegationPosture(posture string) Option {
	return func(b *DefaultBuilder) {
		b.delegationPosture = subagentprompt.NormalizePosture(posture)
	}
}

// New 创建提示词构建器。
func New(injectors ...ContextInjector) Builder {
	return NewWithOptions(nil, injectors...)
}

// NewWithOptions 创建带选项的提示词构建器。
func NewWithOptions(opts []Option, injectors ...ContextInjector) Builder {
	clean := make([]ContextInjector, 0, len(injectors))
	for _, injector := range injectors {
		if injector != nil {
			clean = append(clean, injector)
		}
	}
	b := &DefaultBuilder{injectors: clean, delegationPosture: subagentprompt.PostureProactive}
	for _, opt := range opts {
		if opt != nil {
			opt(b)
		}
	}
	return b
}

// BuildSystem 构建 System 消息。
func (b *DefaultBuilder) BuildSystem(ctx context.Context, req BuildRequest) (string, error) {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("当前时间: %s\n\n", time.Now().Format("2006年01月02日 15:04:05")))

	if req.Agent != nil && req.Agent.SystemPrompt != "" {
		sb.WriteString(req.Agent.SystemPrompt)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("你是一个有帮助的AI助手。请根据用户的问题，合理使用提供的工具来回答。\n\n")
	}

	for _, injector := range b.injectors {
		fragment, err := injector.Inject(ctx, req)
		if err != nil {
			return "", fmt.Errorf("注入提示词片段失败: %w", err)
		}
		if strings.TrimSpace(fragment.Contents) == "" {
			continue
		}
		if fragment.Name != "" {
			sb.WriteString("<")
			sb.WriteString(fragment.Name)
			sb.WriteString(">\n")
		}
		sb.WriteString(strings.TrimRight(fragment.Contents, "\n"))
		sb.WriteString("\n")
		if fragment.Name != "" {
			sb.WriteString("</")
			sb.WriteString(fragment.Name)
			sb.WriteString(">\n")
		}
		sb.WriteString("\n")
	}

	// L0 稳定块：规划模式与任务清单互斥；委派纪律仅主 Agent
	writeCollaborationBlocks(&sb, req)
	if effectiveAudience(req) != AudienceSubAgent {
		writeDelegationBlock(&sb, req.AvailableTools, b.effectiveDelegationPosture(req))
	}
	writeBehaviorRules(&sb, req)

	return sb.String(), nil
}

func effectiveAudience(req BuildRequest) Audience {
	if req.Audience == AudienceSubAgent {
		return AudienceSubAgent
	}
	return AudienceRoot
}

// writeBehaviorRules 按受众裁剪行为规则；子 Run 已含 ChildBase，避免重复腔调。
func writeBehaviorRules(sb *strings.Builder, req BuildRequest) {
	sb.WriteString("## 行为规则\n")
	if effectiveAudience(req) == AudienceSubAgent {
		sb.WriteString("- 使用工具前先判断是否必要\n")
		writeToolBehaviorRules(sb, req.AvailableTools)
		sb.WriteString("- 收到 failure_kind=repeated_failure：禁止再次提交相同调用，必须改参、先完成 suggested_action，或向调用方说明阻塞\n")
		sb.WriteString("- 收到 failure_kind=no_progress：必须总结阻塞并停止无效微调\n")
		return
	}
	sb.WriteString("- 思考时请清晰说明你的推理过程\n")
	sb.WriteString("- 所有文件与目录路径必须使用工作区相对路径（例如 src/main.go、input/data.csv、output/report.pdf），禁止使用包含盘符、根斜杠或宿主机物理路径的绝对路径\n")
	sb.WriteString("- 使用工具前先判断是否必要\n")
	writeToolBehaviorRules(sb, req.AvailableTools)
	sb.WriteString("- 工具结果需要结合上下文给出完整回答\n")
	sb.WriteString("- 直接回答用户的问题，不要重复工具的原始输出\n")
	sb.WriteString("- 收到 failure_kind=repeated_failure：禁止再次提交相同调用，必须改参、先完成 suggested_action，或向用户说明阻塞\n")
	sb.WriteString("- 收到 failure_kind=no_progress：必须总结阻塞或询问用户，禁止继续微调无效调用\n")
}

func (b *DefaultBuilder) effectiveDelegationPosture(req BuildRequest) subagentprompt.Posture {
	if strings.TrimSpace(req.DelegationPosture) != "" {
		return subagentprompt.NormalizePosture(req.DelegationPosture)
	}
	if b != nil && b.delegationPosture != "" {
		return subagentprompt.NormalizePosture(string(b.delegationPosture))
	}
	return subagentprompt.PostureProactive
}

// writeCollaborationBlocks 按协作模式互斥注入 plan_mode_rules 或 task_management。
func writeCollaborationBlocks(sb *strings.Builder, req BuildRequest) {
	mode := collab.Normalize(collab.Mode(req.CollaborationMode))
	if mode == collab.ModePlan {
		sb.WriteString("<plan_mode_rules>\n")
		sb.WriteString(strings.TrimRight(planmodeprompt.SystemRules(req.PlanDocumentPath), "\n"))
		sb.WriteString("\n</plan_mode_rules>\n\n")
		return
	}
	writeTaskManagementBlock(sb, req.AvailableTools)
}

// writeTaskManagementBlock 在 todo_* 工具可用时注入稳定 system 段 <task_management>。
func writeTaskManagementBlock(sb *strings.Builder, availableTools []string) {
	if !hasTools(availableTools, "todo_write", "todo_update_step") {
		return
	}
	sb.WriteString("<task_management>\n")
	sb.WriteString(strings.TrimRight(tasklistprompt.SystemRules, "\n"))
	sb.WriteString("\n</task_management>\n\n")
}

// writeDelegationBlock 在 Task 可用时注入稳定 system 段 <delegation>。
func writeDelegationBlock(sb *strings.Builder, availableTools []string, posture subagentprompt.Posture) {
	if !hasAnyTool(availableTools, "Task") {
		return
	}
	sb.WriteString("<delegation>\n")
	sb.WriteString(strings.TrimRight(subagentprompt.SystemRules(posture), "\n"))
	sb.WriteString("\n</delegation>\n\n")
}

func writeToolBehaviorRules(sb *strings.Builder, availableTools []string) {
	available := make(map[string]struct{}, len(availableTools))
	for _, name := range availableTools {
		available[name] = struct{}{}
	}
	has := func(name string) bool {
		_, ok := available[name]
		return ok
	}

	if has("read_file") {
		sb.WriteString("- 用户给出的裸文件名（如 report.md）按 workspace 根下的精确相对路径处理，直接 read_file，禁止擅自改写为通配路径\n")
	}
	if has("Task") {
		sb.WriteString("- 文件查找工具选择：needle/单文件用 read_file/grep/glob；非 needle 广搜优先 Task(subagent_type=explore)，以节省主上下文\n")
	} else {
		discoveryRules := make([]string, 0, 4)
		if has("glob") {
			discoveryRules = append(discoveryRules, "位置确实未知或需要匹配路径时用 glob")
		}
		if has("list_dir") {
			discoveryRules = append(discoveryRules, "列直接子项用 list_dir")
		}
		if has("walk_dir") {
			discoveryRules = append(discoveryRules, "递归遍历用 walk_dir")
		}
		if has("grep") {
			discoveryRules = append(discoveryRules, "搜索文件内容用 grep")
		}
		if len(discoveryRules) > 0 {
			sb.WriteString("- 文件查找工具选择：")
			sb.WriteString(strings.Join(discoveryRules, "；"))
			sb.WriteString("\n")
		}
	}
	if has("list_dir") {
		sb.WriteString("- list_dir只需名称时使用detail=names；数量必须直接采用returned_count，不得手工计数；truncated=true时必须明确说明结果不完整\n")
		sb.WriteString("- 用户只要求列出名称时，应原样使用工具返回的names，不要擅自补充用途、说明或其他推测信息\n")
	}
	if has("glob") {
		sb.WriteString("- glob 返回 matches 路径数组与 match_count；matches=[] 表示无匹配且成功，禁止据此改用 shell ls 通配重试\n")
	}
	if has("grep") {
		sb.WriteString("- grep 返回 matches 命中数组与 match_count；matches=[] 表示无命中且成功（例如确认无占位符），禁止据此改用 shell grep 重试\n")
	}
	if has("run_command") {
		sb.WriteString("- run_command 仅用于结构化工具无法表达或用户明确要求命令的场景；command 只填写当前默认 Shell 的脚本正文，不要再次嵌套 powershell、cmd /c、bash -lc 等 Shell 启动命令；不得使用 environment_context 未声明支持的 Shell\n")
		avoid := make([]string, 0, 3)
		prefer := make([]string, 0, 3)
		if has("list_dir") || has("glob") {
			avoid = append(avoid, "ls/dir/Get-ChildItem 路径枚举")
			if has("list_dir") {
				prefer = append(prefer, "list_dir")
			}
			if has("glob") {
				prefer = append(prefer, "glob")
			}
		}
		if has("grep") {
			avoid = append(avoid, "grep/rg 文本搜索")
			prefer = append(prefer, "grep")
		}
		if len(avoid) > 0 {
			sb.WriteString("- 禁止用 run_command 做")
			sb.WriteString(strings.Join(avoid, "或"))
			sb.WriteString("；应改用 ")
			sb.WriteString(strings.Join(prefer, "/"))
			sb.WriteString("\n")
		}
		sb.WriteString("- 对抽取管道做「无匹配即成功」的检查时，必须在脚本内显式定义业务退出码，勿依赖 shell grep/ls 的默认非零退出\n")
	}
}

func hasTools(availableTools []string, names ...string) bool {
	available := make(map[string]struct{}, len(availableTools))
	for _, name := range availableTools {
		available[name] = struct{}{}
	}
	for _, name := range names {
		if _, ok := available[name]; !ok {
			return false
		}
	}
	return true
}

func hasAnyTool(availableTools []string, names ...string) bool {
	available := make(map[string]struct{}, len(availableTools))
	for _, name := range availableTools {
		available[name] = struct{}{}
	}
	for _, name := range names {
		if _, ok := available[name]; ok {
			return true
		}
	}
	return false
}
