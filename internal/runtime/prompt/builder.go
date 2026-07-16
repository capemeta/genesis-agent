// Package prompt 负责构建发送给 LLM 的系统提示词
// 支持静态提示词和动态注入（记忆、技能等）
package prompt

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultBuilder 系统提示词构建器。
type DefaultBuilder struct {
	injectors []ContextInjector
}

// New 创建提示词构建器。
func New(injectors ...ContextInjector) Builder {
	clean := make([]ContextInjector, 0, len(injectors))
	for _, injector := range injectors {
		if injector != nil {
			clean = append(clean, injector)
		}
	}
	return &DefaultBuilder{injectors: clean}
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

	sb.WriteString("## 行为规则\n")
	sb.WriteString("- 思考时请清晰说明你的推理过程\n")
	sb.WriteString("- 使用工具前先判断是否必要\n")
	sb.WriteString("- 文件系统操作优先使用结构化工具：列直接子项用 list_dir，递归遍历用 walk_dir，按名称查找用 glob，搜索内容用 grep，读取文件用 read_file；只有结构化工具无法表达或用户明确要求命令时才使用 run_command\n")
	sb.WriteString("- list_dir只需名称时使用detail=names；数量必须直接采用returned_count，不得手工计数；truncated=true时必须明确说明结果不完整\n")
	sb.WriteString("- 用户只要求列出名称时，应原样使用工具返回的names，不要擅自补充用途、说明或其他推测信息\n")
	sb.WriteString("- run_command 的 command 只填写当前默认 Shell 的脚本正文，不要再次嵌套 powershell、cmd /c、bash -lc 等 Shell 启动命令；不得使用 environment_context 未声明支持的 Shell\n")
	sb.WriteString("- 工具结果需要结合上下文给出完整回答\n")
	sb.WriteString("- 直接回答用户的问题，不要重复工具的原始输出\n")
	sb.WriteString("- 收到 failure_kind=repeated_failure：禁止再次提交相同调用，必须改参、先完成 suggested_action，或向用户说明阻塞\n")
	sb.WriteString("- 收到 failure_kind=no_progress：必须总结阻塞或询问用户，禁止继续微调无效调用\n")

	return sb.String(), nil
}
