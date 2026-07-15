package http

import (
	"context"
	"testing"

	"genesis-agent/internal/app"
	connection "genesis-agent/internal/capabilities/connection/contract"
	credential "genesis-agent/internal/capabilities/credential/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/config"
)

func TestNewRouterRegistersPatterns(t *testing.T) {
	t.Parallel()

	defer func() {
		if err := recover(); err != nil {
			t.Fatalf("newRouter panicked: %v", err)
		}
	}()

	if got := newRouter(fakeAgentService{}, nil); got == nil {
		t.Fatal("newRouter returned nil")
	}
}

type fakeAgentService struct{}

func (fakeAgentService) RunOnce(context.Context, app.RunRequest) (*app.RunResult, error) {
	return nil, nil
}

func (fakeAgentService) ClearSession(context.Context, string) error {
	return nil
}

func (fakeAgentService) ListSessionMessages(context.Context, string) ([]*domain.Message, error) {
	return nil, nil
}

func (fakeAgentService) CreateSession(context.Context, app.SessionScope) (*domain.Session, error) {
	return &domain.Session{}, nil
}

func (fakeAgentService) ResumeSession(context.Context, string, app.SessionScope) (*domain.Session, error) {
	return &domain.Session{}, nil
}

func (fakeAgentService) ContinueSession(context.Context, app.SessionScope) (*domain.Session, error) {
	return &domain.Session{}, nil
}

func (fakeAgentService) ListSessions(context.Context, app.SessionScope, int) ([]*domain.Session, error) {
	return nil, nil
}
func (fakeAgentService) ForkSession(context.Context, string, string, app.SessionScope) (*domain.Session, error) {
	return nil, nil
}
func (fakeAgentService) ReplaySession(context.Context, string, string, app.SessionScope) ([]*domain.Message, error) {
	return nil, nil
}

func (fakeAgentService) ListTools() []*tool.Info {
	return nil
}

func (fakeAgentService) Config() *config.Config {
	return nil
}

func (fakeAgentService) DefaultAgent() *domain.Agent {
	return nil
}

func (fakeAgentService) Credentials() credential.Service {
	return nil
}

func (fakeAgentService) Connections() connection.Service {
	return nil
}

func (fakeAgentService) SaveLongTermMemory(context.Context, string, string, string) error {
	return nil
}
func (fakeAgentService) ListLongTermMemories(context.Context, app.SessionScope, domain.MemoryQuery) ([]*domain.LongTermEntry, error) {
	return nil, nil
}
func (fakeAgentService) SaveLongTermMemoryEntry(context.Context, app.SessionScope, *domain.LongTermEntry) error {
	return nil
}
func (fakeAgentService) DeleteLongTermMemories(context.Context, app.SessionScope, []string) error {
	return nil
}
func (fakeAgentService) GetUserProfile(context.Context, app.SessionScope) (*domain.UserProfile, error) {
	return nil, nil
}
func (fakeAgentService) SaveUserProfile(context.Context, app.SessionScope, *domain.UserProfile) error {
	return nil
}
