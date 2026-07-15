package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"genesis-agent/internal/capabilities/mcp/model"
	mcpstack "genesis-agent/internal/capabilities/mcp/stack"
)

// MCPHandler 提供 Enterprise MCP 管理面只读/刷新 API。
type MCPHandler struct {
	stack *mcpstack.Stack
}

// NewMCPHandler 创建 MCP HTTP 处理器；stack 可为 nil（MCP 未启用）。
func NewMCPHandler(stack *mcpstack.Stack) *MCPHandler {
	return &MCPHandler{stack: stack}
}

type mcpServerDTO struct {
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	Origin         string    `json:"origin"`
	Required       bool      `json:"required"`
	ToolCount      int       `json:"tool_count"`
	Error          string    `json:"error,omitempty"`
	ConfigKey      string    `json:"config_key,omitempty"`
	LastConnected  time.Time `json:"last_connected,omitempty"`
	LastHealthPing time.Time `json:"last_health_ping,omitempty"`
	Tools          []string  `json:"tools,omitempty"`
}

type mcpServersResponse struct {
	Servers []mcpServerDTO `json:"servers"`
	Enabled bool           `json:"enabled"`
}

// ListServers GET /v1/mcp/servers
func (h *MCPHandler) ListServers(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.stack == nil || h.stack.Manager == nil {
		writeJSON(w, http.StatusOK, mcpServersResponse{Servers: []mcpServerDTO{}, Enabled: false})
		return
	}
	states := h.stack.Manager.States()
	out := make([]mcpServerDTO, 0, len(states))
	for _, st := range states {
		out = append(out, toServerDTO(st, false))
	}
	writeJSON(w, http.StatusOK, mcpServersResponse{Servers: out, Enabled: true})
}

// GetServer GET /v1/mcp/servers/{name}
func (h *MCPHandler) GetServer(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.stack == nil || h.stack.Manager == nil {
		http.Error(w, `{"error":"mcp not enabled"}`, http.StatusNotFound)
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}
	for _, st := range h.stack.Manager.States() {
		if st.Name == name {
			writeJSON(w, http.StatusOK, toServerDTO(st, true))
			return
		}
	}
	http.Error(w, `{"error":"mcp server not found"}`, http.StatusNotFound)
}

// RefreshServer POST /v1/mcp/servers/{name}/refresh（当前为全量 Refresh，按 name 校验存在性）
func (h *MCPHandler) RefreshServer(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.stack == nil || h.stack.Manager == nil {
		http.Error(w, `{"error":"mcp not enabled"}`, http.StatusNotFound)
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}
	if err := h.stack.Refresh(r.Context()); err != nil {
		http.Error(w, `{"error":"`+escapeJSON(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	for _, st := range h.stack.Manager.States() {
		if st.Name == name {
			writeJSON(w, http.StatusOK, toServerDTO(st, true))
			return
		}
	}
	http.Error(w, `{"error":"mcp server not found after refresh"}`, http.StatusNotFound)
}

func toServerDTO(st model.ServerState, withTools bool) mcpServerDTO {
	dto := mcpServerDTO{
		Name:           st.Name,
		Status:         string(st.Status),
		Origin:         string(st.Origin),
		Required:       st.Required,
		ToolCount:      st.ToolCount,
		Error:          st.Error,
		ConfigKey:      st.ConfigKey,
		LastConnected:  st.LastConnected,
		LastHealthPing: st.LastHealthPing,
	}
	if withTools {
		dto.Tools = make([]string, 0, len(st.Tools))
		for _, t := range st.Tools {
			dto.Tools = append(dto.Tools, t.Name)
		}
	}
	return dto
}

func escapeJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "internal error"
	}
	// Marshal 会加上引号，去掉首尾。
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return "internal error"
}
