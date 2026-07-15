package command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"genesis-agent/internal/app"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime/progress"
)

// runResult run 命令的 JSON 输出结构（--json 模式）
type runResult struct {
	Answer   string `json:"answer"`
	Steps    int    `json:"steps"`
	Tokens   int64  `json:"tokens"`
	Duration string `json:"duration"`
	Status   string `json:"status"`
}

// newRunCmd 创建 run 子命令
// run 命令执行单次 Agent 推理并输出结果，适合脚本调用和非交互场景
func newRunCmd(configDirRef *string, sandboxModeRef *string, factory ServiceFactory) *cobra.Command {
	var (
		jsonOutput   bool
		quiet        bool
		progressMode string
		resumeID     string
	)

	cmd := &cobra.Command{
		Use:   "run <消息>",
		Short: "单次推理执行",
		Long: `向 Agent 发送一条消息，同步等待推理完成并输出结果。

适用于脚本调用、管道操作、批量任务等非交互场景。

示例:
  agent run "现在几点了？"
  agent run "帮我计算 sqrt(144) + 2^10"
  agent run --json "今天星期几？"               JSON 格式输出
  agent run --quiet "北京现在几点？" > out.txt  仅输出最终回答（适合重定向）`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			input := strings.TrimSpace(args[0])
			if input == "" {
				return fmt.Errorf("消息内容不能为空")
			}
			if err := validateProgressMode(progressMode); err != nil {
				return err
			}

			// JSON/quiet 模式不能出现交互式审批提示，避免污染机器可读输出。
			serviceQuiet := quiet || jsonOutput
			svc, err := initService(ctx, factory, configDirRef, serviceQuiet, sandboxModeRef)
			if err != nil {
				return fmt.Errorf("初始化失败: %w", err)
			}

			var session *domain.Session
			if id := strings.TrimSpace(resumeID); id != "" {
				session, err = svc.ResumeSession(ctx, id, app.SessionScope{})
			} else {
				session, err = svc.CreateSession(ctx, app.SessionScope{})
			}
			if err != nil {
				return fmt.Errorf("准备会话失败: %w", err)
			}

			progressSink := runProgressSink(progressMode, quiet, jsonOutput)

			// 非 quiet / 非 JSON 模式：打印执行进度提示
			if !quiet && !jsonOutput {
				fmt.Printf("\n📤 输入: %s\n", input)
				if progressSink == nil {
					fmt.Println("⚙️  Agent 推理中...")
				}
				fmt.Println()
			}

			result, err := svc.RunOnce(ctx, app.RunRequest{
				SessionID:  session.ID,
				TenantID:   session.TenantID,
				UserID:     session.UserID,
				Input:      input,
				OnProgress: progressSink,
			})

			// ── 错误处理 ──────────────────────────────────────────
			if err != nil {
				if jsonOutput {
					out := runResult{Status: "error", Answer: err.Error()}
					data, _ := json.Marshal(out)
					fmt.Println(string(data))
					return nil // JSON 模式下错误不通过 exit code 传递
				}
				return fmt.Errorf("推理失败: %w", err)
			}

			run := result.Run
			elapsed := result.Elapsed

			// ── JSON 输出模式 ──────────────────────────────────────
			if jsonOutput {
				out := runResult{
					Answer:   run.FinalAnswer,
					Steps:    len(run.Steps),
					Tokens:   run.TotalTokens,
					Duration: elapsed.Round(time.Millisecond).String(),
					Status:   string(run.Status),
				}
				data, err := json.MarshalIndent(out, "", "  ")
				if err != nil {
					return fmt.Errorf("序列化输出失败: %w", err)
				}
				fmt.Println(string(data))
				return nil
			}

			// ── Quiet 输出模式（仅回答文本，适合管道）──────────────
			if quiet {
				fmt.Println(run.FinalAnswer)
				return nil
			}

			// ── 正常格式化输出 ─────────────────────────────────────
			answerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F9FAFB"))
			labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#8B5CF6")).Bold(true)
			metaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Italic(true)
			divider := strings.Repeat("─", 60)

			fmt.Println(divider)
			fmt.Printf("%s\n", labelStyle.Render("📝 Agent 回复"))
			fmt.Println(answerStyle.Render(run.FinalAnswer))
			fmt.Println(divider)
			fmt.Printf("   %s\n", metaStyle.Render(
				fmt.Sprintf("%d 步骤 · %d tokens · %v",
					len(run.Steps), run.TotalTokens, elapsed.Round(time.Millisecond)),
			))
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "以 JSON 格式输出结果（适合脚本解析）")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "仅输出最终回答文本（适合管道操作）")
	cmd.Flags().StringVar(&progressMode, "progress", "auto", "进度输出模式：auto|off|text|jsonl（输出到stderr）")
	cmd.Flags().StringVar(&resumeID, "resume", "", "在指定会话中继续单次推理")

	return cmd
}

func runProgressSink(mode string, quiet, jsonOutput bool) progress.Sink {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "auto"
	}
	if mode == "auto" {
		if quiet || jsonOutput {
			return nil
		}
		mode = "text"
	}
	if mode == "off" || mode == "none" {
		return nil
	}
	return func(event progress.Event) {
		if event.Summary == "" && event.Name == "" {
			return
		}
		switch mode {
		case "jsonl":
			data, err := json.Marshal(event)
			if err == nil {
				fmt.Fprintln(os.Stderr, string(data))
			}
		default:
			summary := event.Summary
			if summary == "" {
				summary = string(event.Kind) + ": " + event.Name
			}
			fmt.Fprintf(os.Stderr, "· %s\n", summary)
		}
	}
}

func validateProgressMode(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return nil
	}
	switch mode {
	case "auto", "off", "none", "text", "jsonl":
		return nil
	default:
		return fmt.Errorf("未知progress模式 %q，可选: auto|off|text|jsonl", mode)
	}
}
