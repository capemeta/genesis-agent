package http

import (
	"net/http"

	"genesis-agent/internal/app"
	mcpstack "genesis-agent/internal/capabilities/mcp/stack"
	"genesis-agent/products/enterprise/internal/interfaces/http/handler"
)

// newRouter 创建路由器并注册所有 API 路由
// 使用标准库 http.ServeMux，避免引入不必要的第三方路由框架
// 后期如需要路径参数、中间件等高级特性，可替换为 chi 或 gin
func newRouter(svc app.AgentService, mcp *mcpstack.Stack) http.Handler {
	mux := http.NewServeMux()
	h := handler.NewAgentHandler(svc)
	secrets := handler.NewSecretsHandler(svc)
	mcpHandler := handler.NewMCPHandler(mcp)

	// ── 健康检查 ──────────────────────────────────────────────
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /readiness", handleHealth)

	// ── Agent 核心 API ─────────────────────────────────────────
	// POST /v1/runs          发起一次 Agent 推理（同步，返回最终结果）
	// POST /v1/runs/stream   发起流式推理（SSE，实时推送思考步骤和结果）
	// GET  /v1/tools         获取已注册工具列表
	mux.HandleFunc("POST /v1/runs", h.Run)
	mux.HandleFunc("POST /v1/runs/stream", h.RunStream)
	mux.HandleFunc("GET /v1/tools", h.ListTools)
	mux.HandleFunc("GET /v1/resources/{id}", h.GetResource)

	// ── MCP 管理 API（只读 + refresh；Run 内调用仍走 Tool Gateway）──
	mux.HandleFunc("GET /v1/mcp/servers", mcpHandler.ListServers)
	mux.HandleFunc("GET /v1/mcp/servers/{name}", mcpHandler.GetServer)
	mux.HandleFunc("POST /v1/mcp/servers/{name}/refresh", mcpHandler.RefreshServer)

	// ── 密钥与业务连接 API ─────────────────────────────────────
	// 密钥接口只返回元数据，不回显 secret 明文。
	mux.HandleFunc("POST /v1/credentials", secrets.CreateCredential)
	mux.HandleFunc("GET /v1/credentials", secrets.ListCredentials)
	mux.HandleFunc("GET /v1/credentials/{id}", secrets.GetCredential)
	mux.HandleFunc("PATCH /v1/credentials/{id}", secrets.UpdateCredential)
	mux.HandleFunc("DELETE /v1/credentials/{id}", secrets.DeleteCredential)
	mux.HandleFunc("POST /v1/http-connections", secrets.CreateHTTPConnection)
	mux.HandleFunc("GET /v1/http-connections", secrets.ListHTTPConnections)
	mux.HandleFunc("GET /v1/http-connections/{id}", secrets.GetHTTPConnection)
	mux.HandleFunc("PATCH /v1/http-connections/{id}", secrets.UpdateHTTPConnection)
	mux.HandleFunc("DELETE /v1/http-connections/{id}", secrets.DeleteHTTPConnection)
	mux.HandleFunc("POST /v1/http-connections/{id}/test", secrets.TestHTTPConnection)

	return mux
}

// handleHealth 健康检查端点（k8s liveness/readiness probe）
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
