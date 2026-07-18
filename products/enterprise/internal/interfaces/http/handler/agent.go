// Package handler HTTP 请求处理器
// 每个处理器只负责：参数绑定 → 调用 Service → 序列化响应
// 业务逻辑全部委托给 app.AgentService，保持处理器轻量
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"genesis-agent/internal/app"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	"genesis-agent/internal/runtime/progress"
)

// Resource 存储大文件或大结果的数据结构
type Resource struct {
	Content   string
	CreatedAt time.Time
	TTL       time.Duration
}

// AgentHandler Agent 相关 HTTP 处理器
type AgentHandler struct {
	svc           app.AgentService
	resourceStore sync.Map // 线程安全的资源缓存（resourceID -> Resource）
}

// NewAgentHandler 创建处理器（通过构造函数注入依赖）
func NewAgentHandler(svc app.AgentService) *AgentHandler {
	h := &AgentHandler{svc: svc}
	// 启动定期清理过期资源的 goroutine
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			h.cleanExpiredResources()
		}
	}()
	return h
}

// ── 请求/响应 DTO ──────────────────────────────────────────────

// RunRequest POST /v1/runs 请求体
type RunRequest struct {
	SessionID string                    `json:"session_id"`       // 可选：指定会话 ID，否则自动创建新会话
	AppID     string                    `json:"app_id,omitempty"` // 选择租户策略允许的 Agent App
	UserID    string                    `json:"user_id"`          // 开发期身份占位；生产环境应由认证中间件注入
	Input     string                    `json:"input"`            // 必填：用户输入内容
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
	req.AppID = strings.TrimSpace(req.AppID)
	if req.AppID == "" {
		req.AppID = "enterprise-default"
	}

	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		userID = "user"
	}
	// 创建或使用已有会话
	sessionID := req.SessionID
	if sessionID == "" {
		session, err := h.svc.CreateSession(r.Context(), app.SessionScope{TenantID: "dev", UserID: userID, AppID: req.AppID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("创建会话失败: %v", err))
			return
		}
		sessionID = session.ID
	} else if _, err := h.svc.ResumeSession(r.Context(), sessionID, app.SessionScope{TenantID: "dev", UserID: userID, AppID: req.AppID}); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("会话不可恢复: %v", err))
		return
	}

	result, err := h.svc.RunOnce(r.Context(), app.RunRequest{
		SessionID: sessionID,
		AppID:     req.AppID,
		TenantID:  "dev", // Phase 1A 硬编码租户，Phase 1B 从 JWT 解析
		UserID:    userID,
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
func (h *AgentHandler) RunStream(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("请求体解析失败: %v", err))
		return
	}

	if req.Input == "" {
		writeError(w, http.StatusBadRequest, "input 字段不能为空")
		return
	}
	req.AppID = strings.TrimSpace(req.AppID)
	if req.AppID == "" {
		req.AppID = "enterprise-default"
	}

	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		userID = "user"
	}
	// 创建或使用已有会话
	sessionID := req.SessionID
	if sessionID == "" {
		session, err := h.svc.CreateSession(r.Context(), app.SessionScope{TenantID: "dev", UserID: userID, AppID: req.AppID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("创建会话失败: %v", err))
			return
		}
		sessionID = session.ID
	} else if _, err := h.svc.ResumeSession(r.Context(), sessionID, app.SessionScope{TenantID: "dev", UserID: userID, AppID: req.AppID}); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("会话不可恢复: %v", err))
		return
	}
	tenantID := "dev" // Phase 1A 硬编码租户，Phase 1B 从 JWT 解析

	// 设置 SSE 响应头
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "不支持流式响应 (Flusher unsupported)")
		return
	}

	// 立即写入并 flush 响应头，触发浏览器 SSE 解析模式
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	progressCh := make(chan progress.Event, 256)
	type runResult struct {
		res *app.RunResult
		err error
	}
	resultCh := make(chan runResult, 1)

	// 异步启动推理以允许并发心跳与取消监测
	go func() {
		res, err := h.svc.RunOnce(r.Context(), app.RunRequest{
			SessionID: sessionID,
			AppID:     req.AppID,
			TenantID:  tenantID,
			UserID:    userID,
			Input:     req.Input,
			Sandbox:   req.Sandbox,
			OnProgress: func(event progress.Event) {
				progressCh <- event
			},
		})
		close(progressCh)
		resultCh <- runResult{res: res, err: err}
	}()

	seq := 0
	var finalBlockIndices []int
	hasSentTerminalEvent := false
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()

	var runRes *runResult

	for {
		select {
		case event, ok := <-progressCh:
			if !ok {
				progressCh = nil
				if runRes != nil {
					if runRes.err != nil && !hasSentTerminalEvent {
						seq++
						errData := map[string]interface{}{
							"seq":        seq,
							"run_id":     "",
							"session_id": sessionID,
							"tenant_id":  tenantID,
							"ts":         time.Now().Format(time.RFC3339Nano),
							"error_code": "SYSTEM_ERROR",
							"error_type": "fatal",
							"message":    runRes.err.Error(),
							"retriable":  false,
						}
						_ = writeSSE(w, flusher, seq, "run.failed", errData)
					}
					return
				}
				continue
			}

			eventName, data, _ := h.mapProgressEvent(event, seq+1, sessionID, tenantID, &finalBlockIndices)
			if eventName != "" && data != nil {
				seq++
				if err := writeSSE(w, flusher, seq, eventName, data); err != nil {
					return
				}
				if eventName == "run.completed" || eventName == "run.failed" {
					hasSentTerminalEvent = true
				}
			}

			// 重置心跳计时器
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(15 * time.Second)

		case <-timer.C:
			// Heartbeat ping
			seq++
			pingData := map[string]interface{}{
				"seq":    seq,
				"ts":     time.Now().Format(time.RFC3339Nano),
				"status": "running",
			}
			if err := writeSSE(w, flusher, seq, "ping", pingData); err != nil {
				return
			}
			timer.Reset(15 * time.Second)

		case res := <-resultCh:
			runRes = &res
			resultCh = nil
			if progressCh == nil {
				if runRes.err != nil && !hasSentTerminalEvent {
					seq++
					errData := map[string]interface{}{
						"seq":        seq,
						"run_id":     "",
						"session_id": sessionID,
						"tenant_id":  tenantID,
						"ts":         time.Now().Format(time.RFC3339Nano),
						"error_code": "SYSTEM_ERROR",
						"error_type": "fatal",
						"message":    runRes.err.Error(),
						"retriable":  false,
					}
					_ = writeSSE(w, flusher, seq, "run.failed", errData)
				}
				return
			}

		case <-r.Context().Done():
			// 客户端断开连接
			return
		}
	}
}

// GetResource 处理获取大结果的请求：GET /v1/resources/{id}
func (h *AgentHandler) GetResource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "缺少 resource_id")
		return
	}

	val, ok := h.resourceStore.Load(id)
	if !ok {
		writeError(w, http.StatusNotFound, "资源不存在或已过期")
		return
	}

	res := val.(Resource)
	if time.Since(res.CreatedAt) > res.TTL {
		h.resourceStore.Delete(id)
		writeError(w, http.StatusNotFound, "资源已过期")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="tool_result_%s.txt"`, id))
	w.Header().Set("X-Resource-Size", fmt.Sprintf("%d", len(res.Content)))
	w.Header().Set("X-Resource-Expires", res.CreatedAt.Add(res.TTL).Format(time.RFC3339))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(res.Content))
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

// ── 内部辅助函数 ───────────────────────────────────────────────

func (h *AgentHandler) cleanExpiredResources() {
	h.resourceStore.Range(func(key, value interface{}) bool {
		res, ok := value.(Resource)
		if ok && time.Since(res.CreatedAt) > res.TTL {
			h.resourceStore.Delete(key)
		}
		return true
	})
}

func (h *AgentHandler) mapProgressEvent(event progress.Event, seq int, sessionID, tenantID string, finalBlockIndices *[]int) (string, map[string]interface{}, string) {
	runID := event.RunID
	eventTime := event.Time

	// 1. 运行级别事件 (Run Level)
	if event.Kind == progress.KindRun {
		switch event.Phase {
		case progress.PhaseStart:
			modelName := ""
			if event.Metadata != nil {
				modelName = event.Metadata["model"]
			}
			if modelName == "" {
				modelName = "default"
			}
			fields := map[string]interface{}{
				"mode": "react",
				"agent_config_summary": map[string]interface{}{
					"name":           event.Name,
					"model":          modelName,
					"max_iterations": 50,
				},
			}
			return "run.started", buildPayload(seq, runID, sessionID, tenantID, eventTime, fields), ""
		case progress.PhaseComplete:
			fields := map[string]interface{}{
				"output_block_indices": *finalBlockIndices,
				"usage": map[string]interface{}{
					"input_tokens":  0,
					"output_tokens": 0,
				},
				"duration_ms": 0,
			}
			return "run.completed", buildPayload(seq, runID, sessionID, tenantID, eventTime, fields), ""
		case progress.PhaseError:
			fields := map[string]interface{}{
				"error_code": "SYSTEM_ERROR",
				"error_type": "fatal",
				"message":    event.Detail,
				"retriable":  false,
			}
			return "run.failed", buildPayload(seq, runID, sessionID, tenantID, eventTime, fields), ""
		}
	}

	// 2. 块级别事件 (Block Level)
	if event.BlockIndex != nil {
		if event.BlockType == "final_answer" && event.Phase == progress.PhaseStart {
			*finalBlockIndices = append(*finalBlockIndices, *event.BlockIndex)
		}

		switch event.Phase {
		case progress.PhaseStart:
			display := true
			if event.Display != nil {
				display = *event.Display
			}
			fields := map[string]interface{}{
				"block_index":   *event.BlockIndex,
				"block_type":    event.BlockType,
				"step_index":    0,
				"name":          event.Name,
				"display_label": event.Summary,
				"display":       display,
				"content_type":  event.ContentType,
			}
			if event.StepIndex != nil {
				fields["step_index"] = *event.StepIndex
			}
			return "block.start", buildPayload(seq, runID, sessionID, tenantID, eventTime, fields), ""

		case progress.PhaseProgress:
			deltaType := event.DeltaType
			if deltaType == "" {
				if event.BlockType == "tool_input" || event.BlockType == "tool_result" {
					deltaType = "json_delta"
				} else {
					deltaType = "text_delta"
				}
			}
			fields := map[string]interface{}{
				"block_index": *event.BlockIndex,
				"delta_type":  deltaType,
				"value":       event.Detail,
			}
			return "block.delta", buildPayload(seq, runID, sessionID, tenantID, eventTime, fields), ""

		case progress.PhaseComplete, progress.PhaseError:
			stopReason := event.StopReason
			if stopReason == "" {
				if event.Phase == progress.PhaseError {
					stopReason = "error"
				} else {
					stopReason = "complete"
				}
			}
			fields := map[string]interface{}{
				"block_index": *event.BlockIndex,
				"stop_reason": stopReason,
			}

			// 大结果降级逻辑 (阈值：50KB)
			var largeResultContent string
			if len(event.Detail) > 50*1024 {
				largeResultContent = event.Detail
			}

			if largeResultContent != "" {
				resID := generateResourceID()
				h.resourceStore.Store(resID, Resource{
					Content:   largeResultContent,
					CreatedAt: time.Now(),
					TTL:       24 * time.Hour,
				})

				preview := generatePreview(largeResultContent)
				fields["large_result"] = map[string]interface{}{
					"resource_id": resID,
					"size_bytes":  len(largeResultContent),
					"ttl_seconds": 86400,
					"fetch_url":   fmt.Sprintf("/v1/resources/%s", resID),
					"preview":     preview,
				}
				fields["result_summary"] = "Data size exceeds 50KB, lazy load reference: " + resID
			} else {
				if event.BlockType == "tool_result" {
					fields["result_summary"] = event.Detail
				} else {
					fields["result_summary"] = event.Summary
				}
			}
			return "block.stop", buildPayload(seq, runID, sessionID, tenantID, eventTime, fields), ""
		}
	}

	return "", nil, ""
}

func buildPayload(seq int, runID, sessionID, tenantID string, eventTime time.Time, fields map[string]interface{}) map[string]interface{} {
	payload := make(map[string]interface{})
	payload["seq"] = seq
	payload["run_id"] = runID
	payload["session_id"] = sessionID
	payload["tenant_id"] = tenantID
	if eventTime.IsZero() {
		eventTime = time.Now()
	}
	payload["ts"] = eventTime.Format(time.RFC3339Nano)

	for k, v := range fields {
		payload[k] = v
	}
	return payload
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, seq int, event string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", seq, event, string(jsonData))
	if err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func generateResourceID() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)
	return "res_" + hex.EncodeToString(bytes)
}

func generatePreview(content string) map[string]interface{} {
	lines := strings.Split(content, "\n")
	if len(lines) <= 15 {
		return map[string]interface{}{
			"type": "head",
			"head": content,
		}
	}
	headLines := 5
	tailLines := 5
	if len(lines) < headLines+tailLines {
		return map[string]interface{}{
			"type": "head",
			"head": content,
		}
	}
	head := strings.Join(lines[:headLines], "\n")
	tail := strings.Join(lines[len(lines)-tailLines:], "\n")
	return map[string]interface{}{
		"type":       "head_tail",
		"head":       head,
		"tail":       tail,
		"head_lines": headLines,
		"tail_lines": tailLines,
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, ErrorResponse{Error: msg, Code: code})
}
