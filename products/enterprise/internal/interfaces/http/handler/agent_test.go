package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"genesis-agent/internal/app"
	connection "genesis-agent/internal/capabilities/connection/contract"
	credential "genesis-agent/internal/capabilities/credential/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/config"
	"genesis-agent/internal/runtime/progress"
)

type mockAgentService struct {
	runOnceFunc func(ctx context.Context, req app.RunRequest) (*app.RunResult, error)
}

func (m mockAgentService) RunOnce(ctx context.Context, req app.RunRequest) (*app.RunResult, error) {
	if m.runOnceFunc != nil {
		return m.runOnceFunc(ctx, req)
	}
	return &app.RunResult{
		Run: &domain.Run{
			ID:     "test-run-id",
			Status: domain.RunStatusCompleted,
		},
	}, nil
}

func (mockAgentService) ClearSession(context.Context, string) error { return nil }
func (mockAgentService) ListSessionMessages(context.Context, string) ([]*domain.Message, error) {
	return nil, nil
}
func (mockAgentService) CreateSession(context.Context, app.SessionScope) (*domain.Session, error) {
	return &domain.Session{ID: "sess-test"}, nil
}
func (mockAgentService) ResumeSession(context.Context, string, app.SessionScope) (*domain.Session, error) {
	return &domain.Session{ID: "sess-test"}, nil
}
func (mockAgentService) ContinueSession(context.Context, app.SessionScope) (*domain.Session, error) {
	return &domain.Session{ID: "sess-test"}, nil
}
func (mockAgentService) ListSessions(context.Context, app.SessionScope, int) ([]*domain.Session, error) {
	return nil, nil
}
func (mockAgentService) ForkSession(context.Context, string, string, app.SessionScope) (*domain.Session, error) {
	return nil, nil
}
func (mockAgentService) ReplaySession(context.Context, string, string, app.SessionScope) ([]*domain.Message, error) {
	return nil, nil
}
func (mockAgentService) ListTools() []*tool.Info                                          { return nil }
func (mockAgentService) Config() *config.Config                                           { return nil }
func (mockAgentService) DefaultAgent() *domain.Agent                                      { return nil }
func (mockAgentService) Credentials() credential.Service                                  { return nil }
func (mockAgentService) Connections() connection.Service                                  { return nil }
func (mockAgentService) SaveLongTermMemory(context.Context, string, string, string) error { return nil }
func (mockAgentService) ListLongTermMemories(context.Context, app.SessionScope, domain.MemoryQuery) ([]*domain.LongTermEntry, error) {
	return nil, nil
}
func (mockAgentService) SaveLongTermMemoryEntry(context.Context, app.SessionScope, *domain.LongTermEntry) error {
	return nil
}
func (mockAgentService) DeleteLongTermMemories(context.Context, app.SessionScope, []string) error {
	return nil
}
func (mockAgentService) GetUserProfile(context.Context, app.SessionScope) (*domain.UserProfile, error) {
	return nil, nil
}
func (mockAgentService) SaveUserProfile(context.Context, app.SessionScope, *domain.UserProfile) error {
	return nil
}

func TestRunStreamSSE(t *testing.T) {
	svc := mockAgentService{
		runOnceFunc: func(ctx context.Context, req app.RunRequest) (*app.RunResult, error) {
			if req.OnProgress != nil {
				// Emit a normal event with non-nil BlockIndex
				blockIdx1 := 1
				req.OnProgress(progress.Event{
					Kind:       progress.KindLLM,
					Phase:      progress.PhaseStart,
					RunID:      "run-123",
					BlockIndex: &blockIdx1,
					BlockType:  "final_answer",
					Summary:    "Let's go",
				})
				// Emit a large event that triggers lazy loading
				largeData := strings.Repeat("A", 60*1024) // 60KB
				blockIdx2 := 2
				req.OnProgress(progress.Event{
					Kind:       progress.KindTool,
					Phase:      progress.PhaseComplete,
					RunID:      "run-123",
					BlockIndex: &blockIdx2,
					BlockType:  "tool_result",
					Detail:     largeData,
				})
			}
			return &app.RunResult{
				Run: &domain.Run{
					ID:     "run-123",
					Status: domain.RunStatusCompleted,
				},
			}, nil
		},
	}

	h := NewAgentHandler(svc)
	server := httptest.NewServer(http.HandlerFunc(h.RunStream))
	defer server.Close()

	reqBody, _ := json.Marshal(RunRequest{
		SessionID: "sess-123",
		Input:     "hello",
	})
	resp, err := http.Post(server.URL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got %v", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Expected event-stream header, got %s", ct)
	}

	// Read stream response content
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	content := buf.String()

	if !strings.Contains(content, "event: block.start") {
		t.Errorf("Expected block.start in stream, got: %s", content)
	}
	if !strings.Contains(content, "large_result") {
		t.Errorf("Expected large_result block.stop in stream, got: %s", content)
	}
}

func TestGetResourceDownload(t *testing.T) {
	svc := mockAgentService{}
	h := NewAgentHandler(svc)

	// Pre-populate resource in store
	resID := "res_test_123"
	h.resourceStore.Store(resID, Resource{
		Content:   "ResourceContentSecretData",
		CreatedAt: time.Now(),
		TTL:       5 * time.Second,
	})

	req := httptest.NewRequest("GET", "/v1/resources/res_test_123", nil)
	// Mock ServeMux PathValue setter (or just invoke handler using mux)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/resources/{id}", h.GetResource)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}

	if size := resp.Header.Get("X-Resource-Size"); size != "25" {
		t.Errorf("Expected X-Resource-Size 25, got %s", size)
	}

	body := w.Body.String()
	if body != "ResourceContentSecretData" {
		t.Errorf("Expected body 'ResourceContentSecretData', got '%s'", body)
	}
}
