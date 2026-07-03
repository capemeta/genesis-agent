package sandbox

import (
	"errors"
	"fmt"
)

// ErrorCode 是本地沙箱稳定错误分类。
type ErrorCode string

const (
	ErrCodeInvalidInput       ErrorCode = "invalid_input"
	ErrCodeSandboxUnavailable ErrorCode = "sandbox_unavailable"
	ErrCodePolicyUnsupported  ErrorCode = "sandbox_policy_unsupported"
	ErrCodeSandboxInitFailed  ErrorCode = "sandbox_init_failed"
	ErrCodeSandboxDenied      ErrorCode = "sandbox_denied"
)

// Error 携带稳定 code。
type Error struct {
	Code   ErrorCode
	Err    error
	Reason string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Reason != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Reason)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// WithReason 设置错误原因。
func (e *Error) WithReason(reason string) *Error {
	if e == nil {
		return e
	}
	e.Reason = reason
	return e
}

// NewError 创建本地沙箱错误。
func NewError(code ErrorCode, err error) *Error {
	if err == nil {
		err = errors.New(string(code))
	}
	return &Error{Code: code, Err: err}
}

// CodeOf 返回稳定错误分类。
func CodeOf(err error) ErrorCode {
	var sandboxErr *Error
	if errors.As(err, &sandboxErr) {
		return sandboxErr.Code
	}
	return ""
}

// IsUnavailable 判断是否为沙箱不可用。
func IsUnavailable(err error) bool { return CodeOf(err) == ErrCodeSandboxUnavailable }

// IsPolicyUnsupported 判断是否为策略不支持。
func IsPolicyUnsupported(err error) bool { return CodeOf(err) == ErrCodePolicyUnsupported }
