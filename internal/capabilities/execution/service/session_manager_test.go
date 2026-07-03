package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"genesis-agent/internal/capabilities/execution/contract"
	"genesis-agent/internal/capabilities/execution/model"
)

// mockRunner 模拟底层 PTY 的读写和状态行为。
type mockRunner struct {
	sessions map[string]*mockSession
	mu       sync.Mutex
}

type mockSession struct {
	cmd      model.Command
	stdinBuf []byte
	outputCh chan []byte
	closed   chan struct{}
}

func newMockRunner() *mockRunner {
	return &mockRunner{
		sessions: make(map[string]*mockSession),
	}
}

func (r *mockRunner) StartSession(ctx context.Context, sessionID string, cmd model.Command, opts contract.RunOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sessions[sessionID] = &mockSession{
		cmd:      cmd,
		outputCh: make(chan []byte, 100),
		closed:   make(chan struct{}),
	}
	return nil
}

func (r *mockRunner) WriteStdin(ctx context.Context, sessionID string, data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	sess, ok := r.sessions[sessionID]
	if !ok {
		return errors.New("mock session not found")
	}
	sess.stdinBuf = append(sess.stdinBuf, data...)
	return nil
}

func (r *mockRunner) SubscribeOutput(ctx context.Context, sessionID string) (<-chan []byte, context.CancelFunc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sess, ok := r.sessions[sessionID]
	if !ok {
		return nil, nil, errors.New("mock session not found")
	}

	cancel := func() {
		// 取消订阅存根
	}
	return sess.outputCh, cancel, nil
}

func (r *mockRunner) GetSessionStatus(ctx context.Context, sessionID string) (model.SessionStatus, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sess, ok := r.sessions[sessionID]
	if !ok {
		return "", false, nil
	}

	select {
	case <-sess.closed:
		return model.SessionStatusCompleted, true, nil
	default:
		return model.SessionStatusRunning, true, nil
	}
}

func (r *mockRunner) KillSession(ctx context.Context, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	sess, ok := r.sessions[sessionID]
	if !ok {
		return nil
	}

	select {
	case <-sess.closed:
	default:
		close(sess.closed)
		close(sess.outputCh)
	}
	return nil
}

// TestSessionManager_StartAndSubscribe 验证多路复用广播和输出订阅
func TestSessionManager_StartAndSubscribe(t *testing.T) {
	runner := newMockRunner()
	mgr := NewSessionManager(runner)

	ctx := context.Background()
	sessionID := "test_session"

	err := mgr.StartSession(ctx, sessionID, model.Command{Command: "echo hello"}, contract.RunOptions{})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	// 订阅两个独立的流通道进行多路广播验证
	subCh1, cancel1, err := mgr.SubscribeOutput(ctx, sessionID)
	if err != nil {
		t.Fatalf("Subscribe 1 failed: %v", err)
	}
	defer cancel1()

	subCh2, cancel2, err := mgr.SubscribeOutput(ctx, sessionID)
	if err != nil {
		t.Fatalf("Subscribe 2 failed: %v", err)
	}
	defer cancel2()

	// 模拟物理 PTY 产生输出数据
	runner.mu.Lock()
	sess := runner.sessions[sessionID]
	runner.mu.Unlock()

	testData := []byte("hello from pty\n")
	sess.outputCh <- testData

	// 验证订阅者 1
	select {
	case data := <-subCh1:
		if string(data) != "hello from pty\n" {
			t.Errorf("subCh1 data mismatch: %s", string(data))
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("subCh1 timeout waiting for log")
	}

	// 验证订阅者 2
	select {
	case data := <-subCh2:
		if string(data) != "hello from pty\n" {
			t.Errorf("subCh2 data mismatch: %s", string(data))
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("subCh2 timeout waiting for log")
	}

	// 销毁会话
	_ = mgr.KillSession(ctx, sessionID)
	status, exists, _ := mgr.GetSessionStatus(ctx, sessionID)
	if exists || status == model.SessionStatusRunning {
		t.Error("Session should not exist after KillSession")
	}
}

// TestSessionManager_DynamicScanner 验证 Stdin 行扫描和危险指令拦截
func TestSessionManager_DynamicScanner(t *testing.T) {
	runner := newMockRunner()
	mgr := NewSessionManager(runner)

	ctx := context.Background()
	sessionID := "secure_session"

	err := mgr.StartSession(ctx, sessionID, model.Command{Command: "bash"}, contract.RunOptions{})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	defer func() { _ = mgr.KillSession(ctx, sessionID) }()

	// 1. 发送安全字符数据
	err = mgr.WriteStdin(ctx, sessionID, []byte("echo 123\n"))
	if err != nil {
		t.Errorf("WriteStdin safe command failed: %v", err)
	}

	// 2. 发送危险高危指令（包含破坏性操作 rm -rf / ），行扫描器应当能前置拦截并报错
	err = mgr.WriteStdin(ctx, sessionID, []byte("rm -rf /\n"))
	if err == nil {
		t.Fatal("Expected WriteStdin to block destructive command but it returned nil error")
	}

	if !errors.Is(err, ErrLineScanDeny) && !strings.Contains(err.Error(), "violates policy") {
		t.Errorf("Expected LineScanDeny error, got: %v", err)
	}
}
