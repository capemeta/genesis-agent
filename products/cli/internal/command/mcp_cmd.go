package command

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
	clisandbox "genesis-agent/products/cli/internal/sandbox"
)

// MCPAdmin 是 CLI mcp 子命令所需的管理面端口（由 bootstrap 注入实现）。
type MCPAdmin interface {
	Close() error
	Enabled() bool
	States() []model.ServerState
	Refresh(ctx context.Context) error
	ApprovalStore() contract.ApprovalStore
}

// MCPAdminFactory 由产品分发层注入 MCP 管理面构建方式。
type MCPAdminFactory func(ctx context.Context, opts ServiceOptions) (MCPAdmin, error)

func newMCPCmd(configDirRef *string, sandboxModeRef *string, factory MCPAdminFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "管理 MCP server（list/get/approve/reject/refresh）",
		Long: `管理本地 MCP server 连接与 project 预连接审批。

示例:
  genesis-cli mcp list
  genesis-cli mcp get filesystem
  genesis-cli mcp approve filesystem
  genesis-cli mcp reject filesystem
  genesis-cli mcp refresh
  genesis-cli mcp refresh filesystem`,
		SilenceUsage: true,
	}
	cmd.AddCommand(
		newMCPListCmd(configDirRef, sandboxModeRef, factory),
		newMCPGetCmd(configDirRef, sandboxModeRef, factory),
		newMCPApproveCmd(configDirRef, sandboxModeRef, factory),
		newMCPRejectCmd(configDirRef, sandboxModeRef, factory),
		newMCPRefreshCmd(configDirRef, sandboxModeRef, factory),
	)
	return cmd
}

func newMCPListCmd(configDirRef *string, sandboxModeRef *string, factory MCPAdminFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出 MCP server 状态",
		RunE: func(cmd *cobra.Command, args []string) error {
			admin, err := openMCPAdmin(cmd.Context(), configDirRef, sandboxModeRef, factory)
			if err != nil {
				return err
			}
			defer func() { _ = admin.Close() }()
			if !admin.Enabled() {
				fmt.Println("MCP 未启用或未装配（请检查 configs 中 mcp.enabled）")
				return nil
			}
			states := admin.States()
			if len(states) == 0 {
				fmt.Println("暂无 MCP server")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATUS\tORIGIN\tTOOLS\tREQUIRED\tERROR")
			for _, st := range states {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%v\t%s\n",
					st.Name, st.Status, st.Origin, st.ToolCount, st.Required, truncateMCPErr(st.Error))
			}
			return w.Flush()
		},
	}
}

func newMCPGetCmd(configDirRef *string, sandboxModeRef *string, factory MCPAdminFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "查看单个 MCP server 详情",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			admin, err := openMCPAdmin(cmd.Context(), configDirRef, sandboxModeRef, factory)
			if err != nil {
				return err
			}
			defer func() { _ = admin.Close() }()
			if !admin.Enabled() {
				return fmt.Errorf("MCP 未启用或未装配")
			}
			name := strings.TrimSpace(args[0])
			st, ok := findMCPState(admin.States(), name)
			if !ok {
				return fmt.Errorf("mcp server %q 不存在", name)
			}
			fmt.Printf("name:      %s\n", st.Name)
			fmt.Printf("status:    %s\n", st.Status)
			fmt.Printf("origin:    %s\n", st.Origin)
			fmt.Printf("required:  %v\n", st.Required)
			fmt.Printf("tools:     %d\n", st.ToolCount)
			fmt.Printf("configKey: %s\n", st.ConfigKey)
			if st.Error != "" {
				fmt.Printf("error:     %s\n", st.Error)
			}
			if len(st.Tools) > 0 {
				fmt.Println("tool_list:")
				for _, t := range st.Tools {
					fmt.Printf("  - %s\n", t.Name)
				}
			}
			if store := admin.ApprovalStore(); store != nil {
				if d, found, err := store.Get(cmd.Context(), name); err == nil && found {
					fmt.Printf("approval:  %s\n", d)
				}
			}
			return nil
		},
	}
}

func newMCPApproveCmd(configDirRef *string, sandboxModeRef *string, factory MCPAdminFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "approve <name>",
		Short: "批准 project 来源 MCP server 预连接",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return putMCPApproval(cmd.Context(), configDirRef, sandboxModeRef, factory, args[0], contract.ApprovalApproved)
		},
	}
}

func newMCPRejectCmd(configDirRef *string, sandboxModeRef *string, factory MCPAdminFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "reject <name>",
		Short: "拒绝 project 来源 MCP server 预连接",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return putMCPApproval(cmd.Context(), configDirRef, sandboxModeRef, factory, args[0], contract.ApprovalRejected)
		},
	}
}

func newMCPRefreshCmd(configDirRef *string, sandboxModeRef *string, factory MCPAdminFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "refresh [name]",
		Short: "刷新 MCP catalog 并重连（可选指定 server，当前等价全量 refresh）",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			admin, err := openMCPAdmin(cmd.Context(), configDirRef, sandboxModeRef, factory)
			if err != nil {
				return err
			}
			defer func() { _ = admin.Close() }()
			if !admin.Enabled() {
				return fmt.Errorf("MCP 未启用或未装配")
			}
			if err := admin.Refresh(cmd.Context()); err != nil {
				return err
			}
			if len(args) == 1 {
				name := strings.TrimSpace(args[0])
				st, ok := findMCPState(admin.States(), name)
				if !ok {
					return fmt.Errorf("refresh 后仍找不到 mcp server %q", name)
				}
				fmt.Printf("refreshed %s status=%s tools=%d\n", st.Name, st.Status, st.ToolCount)
				return nil
			}
			fmt.Printf("refreshed %d servers\n", len(admin.States()))
			return nil
		},
	}
}

func openMCPAdmin(ctx context.Context, configDirRef *string, sandboxModeRef *string, factory MCPAdminFactory) (MCPAdmin, error) {
	if factory == nil {
		return nil, fmt.Errorf("CLI MCP admin factory 未配置")
	}
	var sandboxCfg clisandbox.Config
	if sandboxModeRef != nil && strings.TrimSpace(*sandboxModeRef) != "" {
		parsed, err := clisandbox.ParseFlag(*sandboxModeRef)
		if err != nil {
			return nil, err
		}
		sandboxCfg = parsed
	}
	return factory(ctx, ServiceOptions{
		ConfigDirRef:  configDirRef,
		Quiet:         true,
		Sandbox:       sandboxCfg,
		WorkspaceRoot: currentWorkspaceRoot(),
	})
}

func putMCPApproval(ctx context.Context, configDirRef *string, sandboxModeRef *string, factory MCPAdminFactory, name string, decision contract.ApprovalDecision) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("server name 不能为空")
	}
	admin, err := openMCPAdmin(ctx, configDirRef, sandboxModeRef, factory)
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close() }()
	store := admin.ApprovalStore()
	if store == nil {
		return fmt.Errorf("mcp approval store 不可用（请确认 mcp.enabled=true）")
	}
	if err := store.Put(ctx, name, decision); err != nil {
		return err
	}
	if admin.Enabled() {
		_ = admin.Refresh(ctx)
	}
	fmt.Printf("%s %s\n", decision, name)
	return nil
}

func findMCPState(states []model.ServerState, name string) (model.ServerState, bool) {
	for _, st := range states {
		if st.Name == name {
			return st, true
		}
	}
	return model.ServerState{}, false
}

func truncateMCPErr(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}
