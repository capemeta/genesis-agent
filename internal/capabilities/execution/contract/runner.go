// Package contract 定义命令执行能力端口。
package contract

import (
	"context"
	"errors"
	"fmt"
	"time"

	"genesis-agent/internal/capabilities/execution/model"
)

// ErrorCode 是命令执行能力的稳定错误分类。
type ErrorCode string

const (
	ErrCodeInvalidInput       ErrorCode = "invalid_input"
	ErrCodePermissionDenied   ErrorCode = "permission_denied"
	ErrCodeTimeout            ErrorCode = "timeout"
	ErrCodeOutputLimit        ErrorCode = "output_limit"
	ErrCodeRunnerFailed       ErrorCode = "runner_failed"
	ErrCodeSandboxUnavailable ErrorCode = "sandbox_unavailable"
)

// Error 携带稳定 code，方便工具输出和后续 HTTP/审计映射。
type Error struct {
	Code ErrorCode
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewError 创建命令执行分类错误。
func NewError(code ErrorCode, err error) error {
	if err == nil {
		err = errors.New(string(code))
	}
	return &Error{Code: code, Err: err}
}

// CodeOf 返回错误分类。
func CodeOf(err error) ErrorCode {
	var execErr *Error
	if errors.As(err, &execErr) {
		return execErr.Code
	}
	return ""
}

// RunOptions 控制命令执行。
type RunOptions struct {
	Timeout                  time.Duration
	MaxOutputBytes           int64
	Sandbox                  model.SandboxProfile
	Workspace                model.ExecutionWorkspace
	InputArtifacts           []model.InputArtifactRef
	ArtifactCollectionPolicy model.ArtifactCollectionPolicy
}

// CommandRunner 是不带沙箱编排语义的直接命令执行端口。
type CommandRunner interface {
	Run(ctx context.Context, cmd model.Command, opts RunOptions) (*model.Result, error)
}

// SandboxRunner 是 Docker、genesis-sandbox 或平台沙箱的统一执行端口。
type SandboxRunner interface {
	RunInSandbox(ctx context.Context, cmd model.Command, sandbox model.SandboxProfile, opts RunOptions) (*model.Result, error)
}

// ExecutionRunner 是工具层依赖的组合执行端口，由产品 bootstrap 决定 direct 或 sandbox。
type ExecutionRunner interface {
	Run(ctx context.Context, cmd model.Command, opts RunOptions) (*model.Result, error)
}

// InteractiveSessionRunner 是支持伪终端（PTY）长生命周期交互式终端会话的底层接口。
type InteractiveSessionRunner interface {
	// StartSession 启动一个 PTY 交互会话，并指定会话 ID
	StartSession(ctx context.Context, sessionID string, cmd model.Command, opts RunOptions) error
	// WriteStdin 向特定的会话标准输入管道追加字符流
	WriteStdin(ctx context.Context, sessionID string, data []byte) error
	// SubscribeOutput 订阅会话的流式日志输出。调用 cancel 函数可以取消本个多路订阅者通道
	SubscribeOutput(ctx context.Context, sessionID string) (ch <-chan []byte, cancel context.CancelFunc, err error)
	// GetSessionStatus 查询特定会话当前的状态以及是否存在
	GetSessionStatus(ctx context.Context, sessionID string) (model.SessionStatus, bool, error)
	// KillSession 终止终端会话，必须确保以进程树级（Cascade Tree-Kill）级联清理所有衍生子进程
	KillSession(ctx context.Context, sessionID string) error
}
