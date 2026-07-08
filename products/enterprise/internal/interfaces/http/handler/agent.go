// Package handler HTTP 请求处理器
// 每个处理器只负责：参数绑定 → 调用 Service → 序列化响应
// 业务逻辑全部委托给 app.AgentService，保持处理器轻量
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"genesis-agent/internal/app"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// AgentHandler Agent 相关 HTTP 处理器
type AgentHandler struct {
	svc app.AgentService
}

// NewAgentHandler 创建处理器（通过构造函数注入依赖）
func NewAgentHandler(svc app.AgentService) *AgentHandler {
	return &AgentHandler{svc: svc}
}

// ── 请求/响应 DTO ──────────────────────────────────────────────

// RunRequest POST /v1/runs 请求体
type RunRequest struct {
	SessionID string                    `json:"session_id"` // 可选：指定会话 ID，否则自动创建新会话
	Input     string                    `json:"input"`      // 必填：用户输入内容
	Sandbox   *execmodel.SandboxProfile `json:"sandbox,omitempty"`
}

// RunResponse POST /v1/runs 响应体
type RunResponse struct {
	Answer     string `json:"answer"`
	Steps      int    `json:"steps"`
	Tokens     int64  `json:"tokens"`
	DurationMs int64  `json:"duration_ms"` // 毫秒
	Status     string `json:"status"`
}

// ToolInfo GET /v1/tools 工具信息
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ToolsResponse GET /v1/tools 响应体
type ToolsResponse struct {
	Tools []ToolInfo `json:"tools"`
	Count int        `json:"count"`
}

// ErrorResponse 统一错误响应
type ErrorResponse struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}

// ── 处理器方法 ─────────────────────────────────────────────────

// Run 处理同步推理请求：POST /v1/runs
func (h *AgentHandler) Run(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("请求体解析失败: %v", err))
		return
	}

	if req.Input == "" {
		writeError(w, http.StatusBadRequest, "input 字段不能为空")
		return
	}

	// 创建或使用已有会话
	sessionID := req.SessionID
	if sessionID == "" {
		session := h.svc.NewSession()
		sessionID = session.ID
	}

	result, err := h.svc.RunOnce(r.Context(), app.RunRequest{
		SessionID: sessionID,
		TenantID:  "dev", // Phase 1A 硬编码租户，Phase 1B 从 JWT 解析
		Input:     req.Input,
		Sandbox:   req.Sandbox,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("推理失败: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, RunResponse{
		Answer:     result.Run.FinalAnswer,
		Steps:      len(result.Run.Steps),
		Tokens:     result.Run.TotalTokens,
		DurationMs: result.Elapsed.Milliseconds(),
		Status:     string(result.Run.Status),
	})
}

// RunStream 处理流式推理请求（SSE）：POST /v1/runs/stream
// Phase 1A 骨架：暂时不支持真正的流式输出，返回 501
func (h *AgentHandler) RunStream(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "流式 SSE 接口将在 Phase 1B 实现")
}

// ListTools 获取已注册工具列表：GET /v1/tools
func (h *AgentHandler) ListTools(w http.ResponseWriter, r *http.Request) {
	infos := h.svc.ListTools()
	tools := make([]ToolInfo, 0, len(infos))
	for _, info := range infos {
		tools = append(tools, ToolInfo{
			Name:        info.Name,
			Description: info.Description,
		})
	}
	writeJSON(w, http.StatusOK, ToolsResponse{
		Tools: tools,
		Count: len(tools),
	})
}

// ── 工具函数 ──────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, ErrorResponse{Error: msg, Code: code})
}
