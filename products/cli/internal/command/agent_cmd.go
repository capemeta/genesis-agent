package command

import (
	"fmt"
	"os"

	"genesis-agent/internal/capabilities/subagent/service"
	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "agents", Short: "管理项目子智能体定义", RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() }}
	cmd.AddCommand(newAgentValidateCmd(), newAgentListCmd())
	return cmd
}

func newAgentValidateCmd() *cobra.Command {
	return &cobra.Command{Use: "validate", Short: "校验当前工作区 .genesis/agents 中的 Markdown 子智能体定义", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		workspace, err := os.Getwd()
		if err != nil {
			return err
		}
		definitions, err := service.LoadProjectDefinitions(workspace)
		if err != nil {
			return err
		}
		fmt.Printf("校验通过：%d 个 Agent 定义\n", len(definitions))
		return nil
	}}
}
func newAgentListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "列出当前工作区可加载的项目子智能体", RunE: func(cmd *cobra.Command, args []string) error {
		workspace, err := os.Getwd()
		if err != nil {
			return err
		}
		definitions, err := service.LoadProjectDefinitions(workspace)
		if err != nil {
			return err
		}
		for _, definition := range definitions {
			fmt.Printf("%s\t%s\n", definition.Name, definition.Description)
		}
		return nil
	}}
}
