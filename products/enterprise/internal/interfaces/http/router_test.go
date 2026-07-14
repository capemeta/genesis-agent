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

	if got := newRouter(fakeAgentService{}); got == nil {
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

func (fakeAgentService) NewSession() *domain.Session {
	return &domain.Session{}
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
