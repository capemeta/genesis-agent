package domain

import "errors"

// 核心领域错误定义，供整个系统使用
var (
	ErrRunNotFound          = errors.New("run not found")
	ErrRunAlreadyResuming   = errors.New("run already resuming")
	ErrRunPaused            = errors.New("run is paused")
	ErrTaskNotFound         = errors.New("task not found")
	ErrSessionNotFound      = errors.New("session not found")
	ErrPermissionDenied     = errors.New("permission denied")
	ErrInterventionRequired = errors.New("human intervention required")
)
