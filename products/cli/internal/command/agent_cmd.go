package command

import (
	"fmt"
	"os"
	"strings"
	"time"

	subagentmodel "genesis-agent/internal/capabilities/subagent/model"
	"genesis-agent/internal/capabilities/subagent/service"
	"genesis-agent/internal/runtime/multiagent/model"
	clisubagent "genesis-agent/products/cli/internal/subagent"
	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "agents", Short: "管理当前有效的本地子智能体定义", RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() }}
	cmd.AddCommand(newAgentValidateCmd(), newAgentListCmd(), newAgentShowCmd(), newAgentTasksCmd(), newAgentTaskCmd(), newAgentCleanupCmd())
	return cmd
}

func newAgentValidateCmd() *cobra.Command {
	return &cobra.Command{Use: "validate", Short: "校验当前有效的本地 Markdown 子智能体定义", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		definitions, err := loadLocalDefinitions()
		if err != nil {
			return err
		}
		fmt.Printf("校验通过：%d 个 Agent 定义\n", len(definitions))
		return nil
	}}
}
func newAgentListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "列出当前有效的本地子智能体", RunE: func(cmd *cobra.Command, args []string) error {
		definitions, err := loadLocalDefinitions()
		if err != nil {
			return err
		}
		for _, definition := range definitions {
			fmt.Printf("%s\t%s\n", definition.Name, definition.Description)
		}
		return nil
	}}
}

func newAgentShowCmd() *cobra.Command {
	return &cobra.Command{Use: "show <name>", Short: "查看一个当前有效的本地子智能体定义", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		definitions, err := loadLocalDefinitions()
		if err != nil {
			return err
		}
		for _, definition := range definitions {
			if definition.Name != args[0] {
				continue
			}
			fmt.Printf("名称：%s\n描述：%s\n工具：%v\n最大轮次：%d\n最大深度：%d\n最大Token：%d\n最大工具调用：%d\n执行模式：%s\n超时秒数：%d\n\n%s\n", definition.Name, definition.Description, definition.Tools, definition.MaxTurns, definition.MaxDepth, definition.MaxTokens, definition.MaxToolCalls, definition.ExecutionMode, definition.TimeoutSec, definition.SystemPrompt)
			return nil
		}
		return fmt.Errorf("未找到项目 Agent 定义 %q", args[0])
	}}
}

func loadLocalDefinitions() ([]subagentmodel.Definition, error) {
	workspace, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return service.LoadLocalDefinitions(workspace, home)
}

func newAgentTasksCmd() *cobra.Command {
	return &cobra.Command{Use: "tasks", Short: "列出当前工作区保存的子智能体任务", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openSubAgentStore()
		if err != nil {
			return err
		}
		items, err := store.List(cmd.Context())
		if err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "暂无子智能体任务记录")
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "AGENT_ID\tSTATUS\tTYPE\tSUMMARY")
		for _, item := range items {
			instance := item.Stored.Instance
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", instance.AgentID, instance.Status, instance.SubagentType, oneLineSummary(instance))
		}
		return nil
	}}
}

func newAgentTaskCmd() *cobra.Command {
	return &cobra.Command{Use: "task <agent_id>", Short: "查看一个子智能体任务的安全摘要", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openSubAgentStore()
		if err != nil {
			return err
		}
		stored, err := store.Get(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		instance := stored.Instance
		fmt.Fprintf(cmd.OutOrStdout(), "AgentID: %s\nStatus: %s\nType: %s\nParentRun: %s\nSession: %s\n", instance.AgentID, instance.Status, instance.SubagentType, instance.ParentRunID, instance.SessionID)
		if instance.ChildRunID != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "ChildRun: %s\n", instance.ChildRunID)
		}
		if instance.Result != nil {
			result := instance.Result
			fmt.Fprintf(cmd.OutOrStdout(), "ResultID: %s\nResultStatus: %s\n", result.ResultID, result.Status)
			if strings.TrimSpace(result.Summary) != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "\nSummary:\n%s\n", result.Summary)
			}
			if len(result.Artifacts) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "\nArtifacts:")
				for _, artifact := range result.Artifacts {
					fmt.Fprintf(cmd.OutOrStdout(), "- %s\t%s\t%s\n", artifact.ResourceID, artifact.Kind, artifact.Path)
				}
			}
			if result.Error != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "\nError: %s (%s)\n", result.Error.Message, result.Error.Code)
			}
		} else if strings.TrimSpace(instance.Summary) != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "\nSummary:\n%s\n", instance.Summary)
		}
		if strings.TrimSpace(instance.Error) != "" && (instance.Result == nil || instance.Result.Error == nil) {
			fmt.Fprintf(cmd.OutOrStdout(), "\nError: %s\n", instance.Error)
		}
		return nil
	}}
}

func newAgentCleanupCmd() *cobra.Command {
	var days int
	var includeRunning bool
	cmd := &cobra.Command{Use: "cleanup", Short: "清理当前工作区旧的子智能体任务记录", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if days <= 0 {
			return fmt.Errorf("--days 必须大于 0")
		}
		store, err := openSubAgentStore()
		if err != nil {
			return err
		}
		result, err := store.Cleanup(cmd.Context(), clisubagent.CleanupOptions{OlderThan: time.Duration(days) * 24 * time.Hour, IncludeRunning: includeRunning})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "清理完成：删除 %d 条，错误 %d 条\n", result.Deleted, result.Errors)
		return nil
	}}
	cmd.Flags().IntVar(&days, "days", 30, "删除早于指定天数的记录")
	cmd.Flags().BoolVar(&includeRunning, "include-running", false, "同时清理 running 状态记录")
	return cmd
}

func openSubAgentStore() (*clisubagent.FileStore, error) {
	workspace, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return clisubagent.NewFileStore(workspace)
}

func oneLineSummary(instance model.Instance) string {
	summary := instance.Summary
	if instance.Result != nil && strings.TrimSpace(instance.Result.Summary) != "" {
		summary = instance.Result.Summary
	}
	summary = strings.Join(strings.Fields(summary), " ")
	if summary == "" && instance.Error != "" {
		summary = strings.Join(strings.Fields(instance.Error), " ")
	}
	runes := []rune(summary)
	if len(runes) > 80 {
		return string(runes[:80]) + "..."
	}
	return summary
}
