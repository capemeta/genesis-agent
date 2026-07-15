package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
	"github.com/google/uuid"
)

// ClearSession 清空指定会话的短期记忆历史
func (s *agentServiceImpl) ClearSession(ctx context.Context, sessionID string) error {
	ref := memory.SessionRef{SessionID: sessionID}
	return s.memStore.Clear(ctx, ref)
}

// ListSessionMessages 返回短期记忆完整链（EnsureKind）；投影由产品侧完成。
func (s *agentServiceImpl) ListSessionMessages(ctx context.Context, sessionID string) ([]*domain.Message, error) {
	ref := memory.SessionRef{SessionID: sessionID}
	res, err := s.memStore.GetRecent(ctx, ref, memory.RecentOptions{})
	if err != nil {
		return nil, fmt.Errorf("list session messages: %w", err)
	}
	msgs := res.Messages
	for _, m := range msgs {
		if m != nil {
			m.EnsureKind()
		}
	}
	return msgs, nil
}

// CreateSession 创建并持久化新的对话会话。
func (s *agentServiceImpl) CreateSession(ctx context.Context, scope SessionScope) (*domain.Session, error) {
	if s.sessionStore == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	scope = s.normalizeSessionScope(scope)
	now := time.Now().UTC()
	session := &domain.Session{
		ID:        "session-" + uuid.NewString(),
		TenantID:  scope.TenantID,
		AgentID:   scope.AgentID,
		UserID:    scope.UserID,
		AppID:     scope.AppID,
		Status:    domain.SessionStateActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.sessionStore.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return session, nil
}

// ResumeSession 在身份范围内恢复既有会话，拒绝已归档或删除的会话。
func (s *agentServiceImpl) ResumeSession(ctx context.Context, sessionID string, scope SessionScope) (*domain.Session, error) {
	if s.sessionStore == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	scope = s.normalizeSessionScope(scope)
	session, err := s.sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("resume session: %w", err)
	}
	if !sessionMatchesScope(session, scope) {
		return nil, fmt.Errorf("resume session: %w", memory.ErrSessionNotFound)
	}
	if session.Status == domain.SessionStateDeleted || session.Status == domain.SessionStateArchived {
		return nil, fmt.Errorf("resume session: session %q is not resumable", sessionID)
	}
	return session, nil
}

// ContinueSession 恢复身份范围内最近更新的可恢复会话。
func (s *agentServiceImpl) ContinueSession(ctx context.Context, scope SessionScope) (*domain.Session, error) {
	if s.sessionStore == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	scope = s.normalizeSessionScope(scope)
	session, err := s.sessionStore.FindLatestSession(ctx, sessionQuery(scope, 1))
	if err != nil {
		if errors.Is(err, memory.ErrSessionNotFound) {
			return nil, fmt.Errorf("continue session: no resumable session found: %w", err)
		}
		return nil, fmt.Errorf("continue session: %w", err)
	}
	return session, nil
}

// ListSessions 返回身份范围内的最近会话。
func (s *agentServiceImpl) ListSessions(ctx context.Context, scope SessionScope, limit int) ([]*domain.Session, error) {
	if s.sessionStore == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	scope = s.normalizeSessionScope(scope)
	sessions, err := s.sessionStore.ListSessions(ctx, sessionQuery(scope, limit))
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return sessions, nil
}

func (s *agentServiceImpl) ReplaySession(ctx context.Context, sessionID, leafID string, scope SessionScope) ([]*domain.Message, error) {
	if _, err := s.ResumeSession(ctx, sessionID, scope); err != nil {
		return nil, err
	}
	store, ok := s.memStore.(memory.SessionHistoryStore)
	if !ok {
		return nil, fmt.Errorf("session history replay is not supported by configured store")
	}
	scope = s.normalizeSessionScope(scope)
	return store.Replay(ctx, memory.SessionRef{SessionID: sessionID, TenantID: scope.TenantID, UserID: scope.UserID, AppID: scope.AppID}, leafID)
}

func (s *agentServiceImpl) ForkSession(ctx context.Context, sourceSessionID, leafID string, scope SessionScope) (*domain.Session, error) {
	scope = s.normalizeSessionScope(scope)
	if _, err := s.ResumeSession(ctx, sourceSessionID, scope); err != nil {
		return nil, err
	}
	store, ok := s.memStore.(memory.SessionHistoryStore)
	if !ok {
		return nil, fmt.Errorf("session fork is not supported by configured store")
	}
	target, err := s.CreateSession(ctx, scope)
	if err != nil {
		return nil, err
	}
	sourceRef := memory.SessionRef{SessionID: sourceSessionID, TenantID: scope.TenantID, UserID: scope.UserID, AppID: scope.AppID}
	targetRef := memory.SessionRef{SessionID: target.ID, TenantID: scope.TenantID, UserID: scope.UserID, AppID: scope.AppID}
	if err := store.Fork(ctx, sourceRef, targetRef, leafID); err != nil {
		_ = s.sessionStore.DeleteSession(ctx, target.ID)
		return nil, fmt.Errorf("fork session history: %w", err)
	}
	return target, nil
}

func (s *agentServiceImpl) normalizeSessionScope(scope SessionScope) SessionScope {
	if scope.TenantID == "" {
		scope.TenantID = "dev"
	}
	if scope.UserID == "" {
		scope.UserID = "user"
	}
	if scope.AgentID == "" && s.defaultAgent != nil {
		scope.AgentID = s.defaultAgent.ID
	}
	return scope
}

func sessionQuery(scope SessionScope, limit int) memory.SessionQuery {
	return memory.SessionQuery{TenantID: scope.TenantID, UserID: scope.UserID, AgentID: scope.AgentID, AppID: scope.AppID, Limit: limit}
}

func sessionMatchesScope(session *domain.Session, scope SessionScope) bool {
	return session != nil && session.TenantID == scope.TenantID && session.UserID == scope.UserID &&
		session.AgentID == scope.AgentID && session.AppID == scope.AppID
}

// SaveLongTermMemory 保存用户主动记住的长期记忆
func (s *agentServiceImpl) SaveLongTermMemory(ctx context.Context, tenantID, userID, content string) error {
	if s.ltm == nil {
		return nil
	}

	scopeType := domain.MemoryScopeUser
	scopeID := userID
	if userID == "" {
		scopeType = domain.MemoryScopeWorkspace
		scopeID = "global"
	}

	ref := memory.SessionRef{
		TenantID: tenantID,
		UserID:   userID,
	}

	entry := &domain.LongTermEntry{
		ID:         fmt.Sprintf("ltm-%d-user", time.Now().UnixNano()),
		TenantID:   tenantID,
		MemoryType: domain.MemoryTypeSemantic,
		Content:    content,
		Importance: 0.9, // 用户主动记住，设为最高重要性级别
		Scope: domain.MemoryScope{
			Type: scopeType,
			ID:   scopeID,
		},
		Status:         "active",
		Tags:           []string{"user-remember"},
		LastAccessedAt: time.Now(),
	}

	return s.ltm.Save(ctx, ref, []*domain.LongTermEntry{entry})
}

func (s *agentServiceImpl) ListLongTermMemories(ctx context.Context, scope SessionScope, query domain.MemoryQuery) ([]*domain.LongTermEntry, error) {
	if s.ltm == nil {
		return nil, fmt.Errorf("long-term memory is not configured")
	}
	scope = s.normalizeSessionScope(scope)
	query.Scopes = []domain.MemoryScope{{Type: domain.MemoryScopeUser, ID: scope.UserID}}
	return s.ltm.Search(ctx, memory.SessionRef{TenantID: scope.TenantID, UserID: scope.UserID, AppID: scope.AppID}, query)
}

func (s *agentServiceImpl) SaveLongTermMemoryEntry(ctx context.Context, scope SessionScope, entry *domain.LongTermEntry) error {
	if s.ltm == nil || entry == nil {
		return fmt.Errorf("long-term memory is not configured or entry is nil")
	}
	scope = s.normalizeSessionScope(scope)
	entry.TenantID = scope.TenantID
	entry.Scope = domain.MemoryScope{Type: domain.MemoryScopeUser, ID: scope.UserID}
	return s.ltm.Save(ctx, memory.SessionRef{TenantID: scope.TenantID, UserID: scope.UserID, AppID: scope.AppID}, []*domain.LongTermEntry{entry})
}

func (s *agentServiceImpl) DeleteLongTermMemories(ctx context.Context, scope SessionScope, ids []string) error {
	if s.ltm == nil {
		return fmt.Errorf("long-term memory is not configured")
	}
	scope = s.normalizeSessionScope(scope)
	return s.ltm.Delete(ctx, memory.SessionRef{TenantID: scope.TenantID, UserID: scope.UserID, AppID: scope.AppID}, ids)
}

func (s *agentServiceImpl) GetUserProfile(ctx context.Context, scope SessionScope) (*domain.UserProfile, error) {
	if s.userProfiles == nil {
		return nil, fmt.Errorf("user profile store is not configured")
	}
	scope = s.normalizeSessionScope(scope)
	return s.userProfiles.Get(ctx, scope.TenantID, scope.UserID)
}

func (s *agentServiceImpl) SaveUserProfile(ctx context.Context, scope SessionScope, profile *domain.UserProfile) error {
	if s.userProfiles == nil {
		return fmt.Errorf("user profile store is not configured")
	}
	scope = s.normalizeSessionScope(scope)
	return s.userProfiles.Save(ctx, scope.TenantID, scope.UserID, profile)
}
