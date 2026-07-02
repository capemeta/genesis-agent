package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"genesis-agent/internal/app"
	connection "genesis-agent/internal/capabilities/connection/contract"
	credential "genesis-agent/internal/capabilities/credential/contract"
)

type SecretsHandler struct {
	svc app.AgentService
}

func NewSecretsHandler(svc app.AgentService) *SecretsHandler {
	return &SecretsHandler{svc: svc}
}

func (h *SecretsHandler) CreateCredential(w http.ResponseWriter, r *http.Request) {
	var req credential.CreateCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("请求体解析失败: %v", err))
		return
	}
	req.TenantID = defaultTenant(req.TenantID)
	meta, err := h.svc.Credentials().Create(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, meta)
}

func (h *SecretsHandler) ListCredentials(w http.ResponseWriter, r *http.Request) {
	filter := credential.CredentialFilter{
		TenantID:  defaultTenant(r.URL.Query().Get("tenant_id")),
		ProjectID: strings.TrimSpace(r.URL.Query().Get("project_id")),
		Status:    credential.CredentialStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
	}
	items, err := h.svc.Credentials().List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": items, "count": len(items)})
}

func (h *SecretsHandler) GetCredential(w http.ResponseWriter, r *http.Request) {
	meta, err := h.svc.Credentials().GetMeta(r.Context(), credential.CredentialRef{
		TenantID: defaultTenant(r.URL.Query().Get("tenant_id")),
		ID:       r.PathValue("id"),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (h *SecretsHandler) UpdateCredential(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Secret      string            `json:"secret"`
		Description string            `json:"description"`
		Tags        []string          `json:"tags"`
		Metadata    map[string]string `json:"metadata"`
		ExpiresAt   *time.Time        `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("请求体解析失败: %v", err))
		return
	}
	meta, err := h.svc.Credentials().Update(r.Context(), credential.UpdateCredentialRequest{
		Ref: credential.CredentialRef{
			TenantID: defaultTenant(r.URL.Query().Get("tenant_id")),
			ID:       r.PathValue("id"),
		},
		Secret:      body.Secret,
		Description: body.Description,
		Tags:        body.Tags,
		Metadata:    body.Metadata,
		ExpiresAt:   body.ExpiresAt,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (h *SecretsHandler) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Credentials().Delete(r.Context(), credential.CredentialRef{
		TenantID: defaultTenant(r.URL.Query().Get("tenant_id")),
		ID:       r.PathValue("id"),
	}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SecretsHandler) CreateHTTPConnection(w http.ResponseWriter, r *http.Request) {
	var req connection.CreateHTTPRequestConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("请求体解析失败: %v", err))
		return
	}
	req.TenantID = defaultTenant(req.TenantID)
	conn, err := h.svc.Connections().CreateHTTP(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, conn)
}

func (h *SecretsHandler) ListHTTPConnections(w http.ResponseWriter, r *http.Request) {
	filter := connection.Filter{
		TenantID:    defaultTenant(r.URL.Query().Get("tenant_id")),
		ProjectID:   strings.TrimSpace(r.URL.Query().Get("project_id")),
		Environment: strings.TrimSpace(r.URL.Query().Get("environment")),
		Status:      connection.Status(strings.TrimSpace(r.URL.Query().Get("status"))),
	}
	items, err := h.svc.Connections().ListHTTP(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": items, "count": len(items)})
}

func (h *SecretsHandler) GetHTTPConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := h.svc.Connections().GetHTTP(r.Context(), connection.Ref{
		TenantID: defaultTenant(r.URL.Query().Get("tenant_id")),
		ID:       r.PathValue("id"),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, conn)
}

func (h *SecretsHandler) UpdateHTTPConnection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string                  `json:"name"`
		Environment    string                  `json:"environment"`
		BaseURL        string                  `json:"base_url"`
		DefaultHeaders map[string]string       `json:"default_headers"`
		Auth           *connection.AuthConfig  `json:"auth"`
		TimeoutMS      *int                    `json:"timeout_ms"`
		Retry          *connection.RetryPolicy `json:"retry"`
		AllowedTools   []string                `json:"allowed_tools"`
		AllowedAgents  []string                `json:"allowed_agents"`
		Status         connection.Status       `json:"status"`
		Description    string                  `json:"description"`
		Tags           []string                `json:"tags"`
		Metadata       map[string]string       `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("请求体解析失败: %v", err))
		return
	}
	conn, err := h.svc.Connections().UpdateHTTP(r.Context(), connection.UpdateHTTPRequestConnectionRequest{
		Ref: connection.Ref{
			TenantID: defaultTenant(r.URL.Query().Get("tenant_id")),
			ID:       r.PathValue("id"),
		},
		Name:           body.Name,
		Environment:    body.Environment,
		BaseURL:        body.BaseURL,
		DefaultHeaders: body.DefaultHeaders,
		Auth:           body.Auth,
		TimeoutMS:      body.TimeoutMS,
		Retry:          body.Retry,
		AllowedTools:   body.AllowedTools,
		AllowedAgents:  body.AllowedAgents,
		Status:         body.Status,
		Description:    body.Description,
		Tags:           body.Tags,
		Metadata:       body.Metadata,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, conn)
}

func (h *SecretsHandler) DeleteHTTPConnection(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Connections().DeleteHTTP(r.Context(), connection.Ref{
		TenantID: defaultTenant(r.URL.Query().Get("tenant_id")),
		ID:       r.PathValue("id"),
	}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SecretsHandler) TestHTTPConnection(w http.ResponseWriter, r *http.Request) {
	resolved, err := h.svc.Connections().ResolveForHTTP(r.Context(), connection.HTTPResolveRequest{
		TenantID:      defaultTenant(r.URL.Query().Get("tenant_id")),
		ConnectionRef: r.PathValue("id"),
		ToolName:      "connection_test",
		Operation:     "resolve",
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"connection_id": resolved.Connection.ID,
		"base_url":      resolved.BaseURL,
		"has_auth":      resolved.Auth != nil,
		"header_count":  len(resolved.Headers),
		"timeout_ms":    resolved.Timeout.Milliseconds(),
		"has_retry":     resolved.Retry != nil,
	})
}

func defaultTenant(tenantID string) string {
	if strings.TrimSpace(tenantID) == "" {
		return "dev"
	}
	return strings.TrimSpace(tenantID)
}
