package app

import (
	"context"
	"errors"
	"testing"
	"time"

	memory "genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
)

type sessionStoreStub struct {
	sessions map[string]*domain.Session
}

func (s *sessionStoreStub) CreateSession(_ context.Context, session *domain.Session) error {
	if s.sessions == nil {
		s.sessions = make(map[string]*domain.Session)
	}
	copy := *session
	s.sessions[session.ID] = &copy
	return nil
}

func (s *sessionStoreStub) GetSession(_ context.Context, sessionID string) (*domain.Session, error) {
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, memory.ErrSessionNotFound
	}
	copy := *session
	return &copy, nil
}

func (s *sessionStoreStub) ListSessions(_ context.Context, _ memory.SessionQuery) ([]*domain.Session, error) {
	return nil, nil
}

func (s *sessionStoreStub) FindLatestSession(_ context.Context, _ memory.SessionQuery) (*domain.Session, error) {
	return nil, memory.ErrSessionNotFound
}

func (s *sessionStoreStub) UpdateSession(_ context.Context, session *domain.Session) error {
	return s.CreateSession(context.Background(), session)
}

func (s *sessionStoreStub) UpdateStatus(_ context.Context, _ string, _, _ domain.SessionState) (bool, error) {
	return true, nil
}

func (s *sessionStoreStub) DeleteSession(_ context.Context, sessionID string) error {
	delete(s.sessions, sessionID)
	return nil
}

func TestResumeSessionRejectsOutOfScopeSession(t *testing.T) {
	t.Parallel()
	store := &sessionStoreStub{sessions: map[string]*domain.Session{
		"session-other-user": {
			ID:       "session-other-user",
			TenantID: "tenant-a",
			UserID:   "user-b",
			AgentID:  "agent-a",
			Status:   domain.SessionStateActive,
		},
	}}
	service := &agentServiceImpl{
		sessionStore: store,
		defaultAgent: &domain.Agent{ID: "agent-a"},
	}

	_, err := service.ResumeSession(context.Background(), "session-other-user", SessionScope{
		TenantID: "tenant-a",
		UserID:   "user-a",
		AgentID:  "agent-a",
	})
	if !errors.Is(err, memory.ErrSessionNotFound) {
		t.Fatalf("ResumeSession error = %v, want ErrSessionNotFound", err)
	}
}

func TestCreateSessionPersistsNormalizedScope(t *testing.T) {
	t.Parallel()
	store := &sessionStoreStub{}
	service := &agentServiceImpl{
		sessionStore: store,
		defaultAgent: &domain.Agent{ID: "agent-default"},
	}

	session, err := service.CreateSession(context.Background(), SessionScope{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.TenantID != "dev" || session.UserID != "user" || session.AgentID != "agent-default" {
		t.Fatalf("normalized scope = %#v", session)
	}
	if session.ID == "" || session.CreatedAt.IsZero() || session.UpdatedAt.IsZero() || session.Status != domain.SessionStateActive {
		t.Fatalf("created session metadata is incomplete: %#v", session)
	}
	if session.UpdatedAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("updated time is unexpectedly old: %s", session.UpdatedAt)
	}
}

type historyStoreStub struct{ messages map[string][]*domain.Message }

func (s *historyStoreStub) Append(_ context.Context, ref memory.SessionRef, messages []*domain.Message) error {
	s.messages[ref.SessionID] = append(s.messages[ref.SessionID], messages...)
	return nil
}
func (s *historyStoreStub) GetRecent(_ context.Context, ref memory.SessionRef, _ memory.RecentOptions) (memory.RecentResult, error) {
	return memory.RecentResult{Messages: s.messages[ref.SessionID]}, nil
}
func (s *historyStoreStub) Summarize(context.Context, memory.SessionRef, memory.SummarizeOptions) (memory.SummaryResult, error) {
	return memory.SummaryResult{}, nil
}
func (s *historyStoreStub) GetSummary(context.Context, memory.SessionRef) (*domain.SessionSummary, error) {
	return nil, nil
}
func (s *historyStoreStub) Clear(context.Context, memory.SessionRef) error { return nil }
func (s *historyStoreStub) Replay(ctx context.Context, ref memory.SessionRef, leaf string) ([]*domain.Message, error) {
	result, err := s.GetRecent(ctx, ref, memory.RecentOptions{})
	return result.Messages, err
}
func (s *historyStoreStub) Fork(_ context.Context, source, target memory.SessionRef, _ string) error {
	s.messages[target.SessionID] = append([]*domain.Message(nil), s.messages[source.SessionID]...)
	return nil
}

func TestForkSessionUsesScopedHistoryStore(t *testing.T) {
	store := &sessionStoreStub{}
	history := &historyStoreStub{messages: map[string][]*domain.Message{"source": {domain.NewUserMessage("hello")}}}
	service := &agentServiceImpl{sessionStore: store, memStore: history, defaultAgent: &domain.Agent{ID: "agent-a"}}
	if _, err := service.CreateSession(context.Background(), SessionScope{TenantID: "tenant-a", UserID: "user-a", AgentID: "agent-a"}); err != nil {
		t.Fatal(err)
	}
	store.sessions["source"] = &domain.Session{ID: "source", TenantID: "tenant-a", UserID: "user-a", AgentID: "agent-a", Status: domain.SessionStateActive}
	fork, err := service.ForkSession(context.Background(), "source", "", SessionScope{TenantID: "tenant-a", UserID: "user-a", AgentID: "agent-a"})
	if err != nil {
		t.Fatalf("ForkSession: %v", err)
	}
	if len(history.messages[fork.ID]) != 1 {
		t.Fatalf("fork history missing")
	}
}
