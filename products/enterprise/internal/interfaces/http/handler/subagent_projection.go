package handler

import (
	"net/http"
	"strconv"
	"strings"

	multicontract "genesis-agent/internal/runtime/multiagent/contract"
	multiagentmodel "genesis-agent/internal/runtime/multiagent/model"
)

// SubAgentProjectionHandler 提供只读的子任务控制面投影；不会返回 transcript 或工具原始输出。
type SubAgentProjectionHandler struct {
	reader   multicontract.ProjectionReader
	tenantID string
}

func NewSubAgentProjectionHandler(reader multicontract.ProjectionReader) *SubAgentProjectionHandler {
	return NewSubAgentProjectionHandlerForTenant(reader, "")
}

// NewSubAgentProjectionHandlerForTenant 创建绑定服务端租户范围的读取器。
// HTTP 参数不能决定租户，生产环境应由认证中间件在此处注入身份范围。
func NewSubAgentProjectionHandlerForTenant(reader multicontract.ProjectionReader, tenantID string) *SubAgentProjectionHandler {
	return &SubAgentProjectionHandler{reader: reader, tenantID: strings.TrimSpace(tenantID)}
}

// List GET /v1/subagents/events
func (h *SubAgentProjectionHandler) List(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.reader == nil {
		writeJSON(w, http.StatusOK, map[string]any{"events": []multiagentmodel.ProjectionEvent{}, "enabled": false})
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("tenant_id")) != "" {
		writeError(w, http.StatusBadRequest, "tenant_id 必须由认证上下文注入")
		return
	}
	if h.tenantID == "" {
		writeError(w, http.StatusServiceUnavailable, "子任务投影尚未配置租户范围")
		return
	}
	limit := 100
	if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 200 {
			writeError(w, http.StatusBadRequest, "limit 必须在 1 到 200 之间")
			return
		}
		limit = parsed
	}
	events, err := h.reader.ListProjectionEvents(r.Context(), multiagentmodel.ProjectionQuery{
		TenantID:  h.tenantID,
		SessionID: strings.TrimSpace(r.URL.Query().Get("session_id")),
		AgentID:   strings.TrimSpace(r.URL.Query().Get("agent_id")),
		Limit:     limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询子任务投影失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "enabled": true})
}
