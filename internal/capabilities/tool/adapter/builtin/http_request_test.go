package builtin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	connection "genesis-agent/internal/capabilities/connection/contract"
	platformhttp "genesis-agent/internal/platform/httpclient"
)

func TestHTTPRequestToolExecute(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "hello" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"method": r.Method,
		})
	}))
	defer server.Close()

	tool := NewHTTPRequestTool(platformhttp.New())
	params := map[string]any{
		"method": "POST",
		"url":    server.URL,
		"query": map[string]any{
			"q": "hello",
		},
		"body": map[string]any{
			"name": "genesis",
		},
		"expected_status": []int{201},
		"auth": map[string]any{
			"type":  "bearer_token",
			"token": "secret-token",
		},
	}

	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	result, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if int(payload["status_code"].(float64)) != http.StatusCreated {
		t.Fatalf("unexpected status code: %v", payload["status_code"])
	}

	bodyJSON, ok := payload["body_json"].(map[string]any)
	if !ok {
		t.Fatalf("body_json missing: %#v", payload)
	}
	if bodyJSON["ok"] != true {
		t.Fatalf("unexpected body_json: %#v", bodyJSON)
	}
}

func TestHTTPRequestToolExecuteWithConnectionRef(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/orders" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-From-Connection"); got != "yes" {
			t.Fatalf("connection header missing: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	tool := NewHTTPRequestTool(platformhttp.New(), fakeHTTPResolver{baseURL: server.URL})
	params := map[string]any{
		"tenant_id":      "dev",
		"connection_ref": "orders",
		"method":         "GET",
		"path":           "/v1/orders",
		"headers": map[string]any{
			"X-Request-Header": "ok",
		},
	}

	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	result, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if int(payload["status_code"].(float64)) != http.StatusOK {
		t.Fatalf("unexpected status code: %v", payload["status_code"])
	}
}

func TestHTTPRequestToolMultipartUpload(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv(httpToolWorkspaceEnv, workspace)
	if err := os.WriteFile(filepath.Join(workspace, "upload.txt"), []byte("hello-upload"), 0600); err != nil {
		t.Fatalf("write upload file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Fatalf("unexpected content type: %s", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm() error = %v", err)
		}
		if got := r.FormValue("kind"); got != "report" {
			t.Fatalf("unexpected field: %q", got)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("FormFile() error = %v", err)
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if string(data) != "hello-upload" {
			t.Fatalf("unexpected file body: %q", string(data))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"uploaded":true}`))
	}))
	defer server.Close()

	params := map[string]any{
		"method": "POST",
		"url":    server.URL,
		"multipart": map[string]any{
			"fields": map[string]any{"kind": "report"},
			"files": []map[string]any{
				{"field": "file", "path": "upload.txt", "filename": "report.txt", "content_type": "text/plain"},
			},
		},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	if _, err := NewHTTPRequestTool(platformhttp.New()).Execute(context.Background(), string(raw)); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestHTTPRequestToolMultipartRejectsTooLargeBody(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv(httpToolWorkspaceEnv, workspace)
	if err := os.WriteFile(filepath.Join(workspace, "large.txt"), []byte("0123456789"), 0600); err != nil {
		t.Fatalf("write upload file: %v", err)
	}

	params := map[string]any{
		"method":                 "POST",
		"url":                    "http://127.0.0.1/upload",
		"max_request_body_bytes": 8,
		"multipart": map[string]any{
			"files": []map[string]any{
				{"field": "file", "path": "large.txt"},
			},
		},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	if _, err := NewHTTPRequestTool(platformhttp.New()).Execute(context.Background(), string(raw)); err == nil {
		t.Fatal("expected oversized multipart body to be rejected")
	}
}
func TestHTTPRequestToolDownload(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv(httpToolWorkspaceEnv, workspace)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("download-body"))
	}))
	defer server.Close()

	params := map[string]any{
		"method": "GET",
		"url":    server.URL,
		"download": map[string]any{
			"save_as":   "downloads/file.bin",
			"max_bytes": 1024,
		},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	result, err := NewHTTPRequestTool(platformhttp.New()).Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if _, ok := payload["body_text"]; ok {
		t.Fatalf("body_text should be omitted for download: %#v", payload)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "downloads", "file.bin"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "download-body" {
		t.Fatalf("unexpected download body: %q", string(data))
	}
}

func TestHTTPRequestToolDownloadRejectsSymlinkTarget(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv(httpToolWorkspaceEnv, workspace)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	link := filepath.Join(workspace, "link.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("new-body"))
	}))
	defer server.Close()

	params := map[string]any{
		"method": "GET",
		"url":    server.URL,
		"download": map[string]any{
			"save_as": "link.txt",
		},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	if _, err := NewHTTPRequestTool(platformhttp.New()).Execute(context.Background(), string(raw)); err == nil {
		t.Fatal("expected symlink download target to be rejected")
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("read outside file: %v", err)
	}
	if string(data) != "outside" {
		t.Fatalf("outside file was modified: %q", string(data))
	}
}
func TestResolveHTTPToolPathRejectsTraversal(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv(httpToolWorkspaceEnv, workspace)
	if _, err := resolveHTTPToolPath("..\\secret.txt"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

type fakeHTTPResolver struct {
	baseURL string
}

func (r fakeHTTPResolver) ResolveForHTTP(context.Context, connection.HTTPResolveRequest) (*connection.ResolvedHTTPConnection, error) {
	return &connection.ResolvedHTTPConnection{
		BaseURL: r.baseURL,
		Headers: http.Header{
			"X-From-Connection": []string{"yes"},
		},
		Timeout: 3 * time.Second,
	}, nil
}
