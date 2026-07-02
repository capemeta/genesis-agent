// Package freshness 提供文件新鲜度追踪。
package freshness

import (
	"context"

	"genesis-agent/internal/capabilities/filesystem/model"
)

// Check 描述写前新鲜度检查结果。
type Check struct {
	Fresh  bool   `json:"fresh"`
	Reason string `json:"reason,omitempty"`
}

// Tracker 记录读写快照，避免覆盖外部修改。
type Tracker interface {
	RecordRead(ctx context.Context, path model.ResolvedPath, stat model.FileStat, hash string) error
	CheckBeforeWrite(ctx context.Context, path model.ResolvedPath, current model.FileStat, currentHash string, expectedHash string) (*Check, error)
	RecordWrite(ctx context.Context, path model.ResolvedPath, stat model.FileStat, hash string) error
}
