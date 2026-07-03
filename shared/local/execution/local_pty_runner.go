package execution

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// activeSession 描述活动中的物理进程会话包装
type activeSession struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	outputCh chan []byte
	cancel   context.CancelFunc
	done     chan struct{}
}

// LocalPTYRunner 在宿主机本地执行 PTY 交互会话
type LocalPTYRunner struct {
	sessions map[string]*activeSession
	mu       sync.RWMutex
}

// NewLocalPTYRunner 创建本地 PTY 驱动器
func NewLocalPTYRunner() *LocalPTYRunner {
	return &LocalPTYRunner{
		sessions: make(map[string]*activeSession),
	}
}

// WriteStdin 向特定的本地进程会话标准输入管道物理追加字节
func (r *LocalPTYRunner) WriteStdin(ctx context.Context, sessionID string, data []byte) error {
	r.mu.RLock()
	sess, exists := r.sessions[sessionID]
	r.mu.RUnlock()

	if !exists {
		return fmt.Errorf("session [%s] not found", sessionID)
	}

	_, err := sess.stdin.Write(data)
	return err
}

// SubscribeOutput 获取会话流式日志
func (r *LocalPTYRunner) SubscribeOutput(ctx context.Context, sessionID string) (<-chan []byte, context.CancelFunc, error) {
	r.mu.RLock()
	sess, exists := r.sessions[sessionID]
	r.mu.RUnlock()

	if !exists {
		return nil, nil, fmt.Errorf("session [%s] not found", sessionID)
	}

	return sess.outputCh, sess.cancel, nil
}

// GetSessionStatus 获取会话状态
func (r *LocalPTYRunner) GetSessionStatus(ctx context.Context, sessionID string) (execmodel.SessionStatus, bool, error) {
	r.mu.RLock()
	sess, exists := r.sessions[sessionID]
	r.mu.RUnlock()

	if !exists {
		return "", false, nil
	}

	select {
	case <-sess.done:
		return execmodel.SessionStatusCompleted, true, nil
	default:
		return execmodel.SessionStatusRunning, true, nil
	}
}

// KillSession 终止物理进程及其子进程树
func (r *LocalPTYRunner) KillSession(ctx context.Context, sessionID string) error {
	r.mu.Lock()
	sess, exists := r.sessions[sessionID]
	delete(r.sessions, sessionID)
	r.mu.Unlock()

	if !exists {
		return nil
	}

	sess.cancel()

	// 树级终止清理（Cascade Tree-Kill）
	return killProcessTree(sess.cmd)
}
