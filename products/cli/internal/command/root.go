// Package command CLI 产品命令层：基于 Cobra + Bubble Tea 实现命令行交互界面
// 依赖 internal/app 应用服务层，不直接访问 engine / memory 等领域包
package command

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"genesis-agent/internal/app"
	tasklistcontract "genesis-agent/internal/capabilities/tasklist/contract"
	"genesis-agent/internal/runtime/collab"
	clisandbox "genesis-agent/products/cli/internal/sandbox"
)

// defaultConfigDir 默认配置目录（相对于工作目录）
const defaultConfigDir = "configs"

// ServiceOptions 描述 CLI 命令初始化 AgentService 时需要传给产品 bootstrap 的参数。
type ServiceOptions struct {
	ConfigDirRef  *string
	Quiet         bool
	Sandbox       clisandbox.Config
	WorkspaceRoot string
}

// ServiceHandle 显式绑定 AgentService 与其运行时生命周期。
// 命令结束必须 Close，避免后台续租、MCP 连接和日志句柄泄漏。
type ServiceHandle interface {
	Service() app.AgentService
	Close() error
}

// CollabHandle 可选扩展：规划模式 ModeStore 与工作区根（CLI TUI 使用）。
type CollabHandle interface {
	CollabStore() collab.Store
	WorkspaceRoot() string
}

// TaskListHandle 可选扩展：任务清单持久化 Repository（CLI TUI 使用）。
type TaskListHandle interface {
	TaskListRepository() tasklistcontract.Repository
}

// ServiceFactory 由产品分发层注入可关闭的 AgentService 运行时。
type ServiceFactory func(ctx context.Context, opts ServiceOptions) (ServiceHandle, error)

// ExecuteWithFactory 使用产品分发层注入的 service factory 执行 CLI。
func ExecuteWithFactory(factory ServiceFactory) error {
	return ExecuteWithFactories(factory, nil)
}

// ExecuteWithFactories 注入 AgentService 与可选 MCP 管理面 factory。
func ExecuteWithFactories(factory ServiceFactory, mcpFactory MCPAdminFactory) error {
	if factory == nil {
		return fmt.Errorf("CLI service factory 未配置，请通过产品入口注入 bootstrap")
	}
	return newRootCmd(factory, mcpFactory).Execute()
}

func initService(ctx context.Context, factory ServiceFactory, configDirRef *string, quiet bool, sandboxModeRef *string) (ServiceHandle, error) {
	var sandboxCfg clisandbox.Config
	var err error
	if sandboxModeRef != nil && strings.TrimSpace(*sandboxModeRef) != "" {
		sandboxCfg, err = clisandbox.ParseFlag(*sandboxModeRef)
		if err != nil {
			return nil, err
		}
	}
	handle, err := factory(ctx, ServiceOptions{ConfigDirRef: configDirRef, Quiet: quiet, Sandbox: sandboxCfg, WorkspaceRoot: currentWorkspaceRoot()})
	if err != nil {
		return nil, err
	}
	if handle == nil || handle.Service() == nil {
		if handle != nil {
			_ = handle.Close()
		}
		return nil, fmt.Errorf("CLI service factory 返回了空 ServiceHandle")
	}
	return handle, nil
}

func closeServiceHandle(handle ServiceHandle, runErr *error) {
	if handle == nil {
		return
	}
	if err := handle.Close(); err != nil && runErr != nil && *runErr == nil {
		*runErr = fmt.Errorf("关闭 CLI 运行时失败: %w", err)
	}
}

func currentWorkspaceRoot() string {
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		return "."
	}
	abs, err := filepath.Abs(wd)
	if err != nil {
		return filepath.Clean(wd)
	}
	return filepath.Clean(abs)
}

// newRootCmd 构建 Cobra 根命令树，注册全局 flag 和所有子命令
func newRootCmd(factory ServiceFactory, mcpFactory MCPAdminFactory) *cobra.Command {
	// configDir / sandboxMode 由持久 flag 控制。
	// 传递指针给各子命令，保证 flag 解析后子命令执行时读取的是最新值。
	configDir := defaultConfigDir
	sandboxMode := ""

	root := &cobra.Command{
		Use:   "genesis-cli",
		Short: "Genesis Agent — 生产级 AI Agent Runtime",
		Long: `Genesis Agent 是一个可扩展的 AI Agent Runtime，支持：

  · ReAct Loop 推理策略（Think → Action → Observe 循环）
  · 多工具调用（calculator、current_time 及自定义扩展）
  · 多轮对话记忆（Short-Term Memory，跨 Run 持久）
  · 多 LLM 服务商（OpenAI 兼容接口、火山引擎 Ark、Ollama）

快速开始:
  genesis-cli chat                         交互式 TUI 对话（推荐）
  genesis-cli run "现在几点了？"             单次推理，输出最终回答
  genesis-cli run --json "计算 sqrt(144)"   JSON 格式输出（适合脚本）
  genesis-cli config                       查看当前配置信息
  genesis-cli tools                        列出已注册的可用工具
  genesis-cli version                      显示版本信息`,
		SilenceUsage:  true, // 出错时不打印 usage（错误信息已足够）
		SilenceErrors: true, // 由 main.go 统一处理错误输出
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// 全局持久 flag：所有子命令均可使用
	root.PersistentFlags().StringVarP(
		&configDir, "config", "c", defaultConfigDir,
		"配置目录路径（含 config.yaml、llm.yaml，以及可选的 mcp.yaml、hooks.yaml、config.local.yaml）",
	)
	root.PersistentFlags().StringVar(
		&sandboxMode, "sandbox", sandboxMode,
		"本次会话命令执行沙箱策略覆盖：disabled、optional 或 required；空值使用配置文件 sandbox.default_execution",
	)

	// 注册所有子命令，统一传入 configDir / sandboxMode 指针
	root.AddCommand(
		newChatCmd(&configDir, &sandboxMode, factory),
		newRunCmd(&configDir, &sandboxMode, factory),
		newConfigCmd(&configDir),
		newHookCmd(&configDir),
		newConfigureCmd(),
		newToolsCmd(&configDir, &sandboxMode, factory),
		newMCPCmd(&configDir, &sandboxMode, mcpFactory),
		newSkillCmd(),
		newPackageCmd(),
		newCapabilityCmd(),
		newAgentCmd(),
		newPluginCmd(),
		newSandboxCmd(),
		newVersionCmd(),
	)

	return root
}
