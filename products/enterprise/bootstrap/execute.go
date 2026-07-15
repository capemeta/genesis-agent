package bootstrap

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	transporthttp "genesis-agent/products/enterprise/internal/interfaces/http"
)

// Execute 启动 Enterprise HTTP 产品入口。
func Execute(ctx context.Context) error {
	return newRootCmd(ctx).Execute()
}

func newRootCmd(parent context.Context) *cobra.Command {
	var (
		configDir string
		host      string
		port      int
	)

	cmd := &cobra.Command{
		Use:   "genesis-enterprise",
		Short: "Genesis Agent Enterprise HTTP API 服务器",
		Long: `启动 Genesis Agent Enterprise 的 RESTful HTTP API 服务，提供以下接口:

  POST /v1/runs         发起 Agent 推理（同步，等待完整结果）
  POST /v1/runs/stream  发起流式推理（SSE，实时推送 Step）[Phase 1B]
  GET  /v1/tools        获取已注册工具列表
  GET  /v1/mcp/servers  列出 MCP server 状态
  GET  /health          健康检查（k8s liveness probe）
  GET  /readiness       就绪检查（k8s readiness probe）

环境变量:
  AGENT_LLM_PROVIDERS_<PROVIDER>_AUTH_API_KEY  覆盖指定 Provider 的 API Key
  AGENT_LLM_PROVIDERS_<PROVIDER>_BASE_URL      覆盖指定 Provider 的服务地址`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewContainer(&configDir, false)

			ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			if err := c.Init(ctx); err != nil {
				return fmt.Errorf("初始化失败: %w", err)
			}

			cfg := transporthttp.DefaultServerConfig()
			if host != "" {
				cfg.Host = host
			}
			if port != 0 {
				cfg.Port = port
			}

			server := transporthttp.NewServerWithMCP(c.Service(), c.MCPStack(), cfg)

			fmt.Printf("Genesis Agent Enterprise HTTP API 已启动: %s\n", server.Addr())
			fmt.Println("按 Ctrl+C 优雅停止服务...")

			err := server.Start(ctx)
			_ = c.Close()
			return err
		},
	}

	cmd.Flags().StringVarP(&configDir, "config", "c", "configs", "配置目录路径（含 config.yaml、llm.yaml，以及可选的 mcp.yaml 和 hooks.yaml）")
	cmd.Flags().StringVar(&host, "host", "", "监听地址（默认 0.0.0.0）")
	cmd.Flags().IntVarP(&port, "port", "p", 0, "监听端口（默认 8080）")

	return cmd
}
