package contract

import (
	"errors"
	"fmt"
)

// ErrorCode 是文件系统能力的稳定错误分类。
type ErrorCode string

const (
	ErrCodeInvalidPath        ErrorCode = "invalid_path"
	ErrCodePermissionDenied   ErrorCode = "permission_denied"
	ErrCodeNotFound           ErrorCode = "not_found"
	ErrCodeAlreadyExists      ErrorCode = "already_exists"
	ErrCodeNotDirectory       ErrorCode = "not_directory"
	ErrCodeTooLarge           ErrorCode = "too_large"
	ErrCodeModifiedExternally ErrorCode = "file_modified_externally"
	ErrCodeLimitExceeded      ErrorCode = "limit_exceeded"
	ErrCodeInvalidInput       ErrorCode = "invalid_input"
)

// Error 携带稳定 code，方便工具输出和后续 HTTP/审计映射。
type Error struct {
	Code ErrorCode
	Path string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Path == "" {
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	}
	return fmt.Sprintf("%s [%s]: %v", e.Code, e.Path, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewError 创建文件系统分类错误。
func NewError(code ErrorCode, path string, err error) error {
	if err == nil {
		err = errors.New(string(code))
	}
	return &Error{Code: code, Path: path, Err: err}
}

// CodeOf 返回错误分类。
func CodeOf(err error) ErrorCode {
	var fsErr *Error
	if errors.As(err, &fsErr) {
		return fsErr.Code
	}
	return ""
}
