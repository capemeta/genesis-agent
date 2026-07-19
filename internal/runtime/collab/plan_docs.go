package collab

import (
	"context"
	"fmt"
	"sync"
)

// PlanDocuments 实施方案文件读写 Port（产品注入本地文件 / 远程 FS / DB 实现）。
type PlanDocuments interface {
	Write(ctx context.Context, sessionID, content string) (relPath string, err error)
	Read(ctx context.Context, sessionID string) (relPath string, content string, err error)
}

// MemoryPlanDocuments 进程内实施方案存储，供测试与未注入产品时使用。
type MemoryPlanDocuments struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewMemoryPlanDocuments 创建内存实施方案存储。
func NewMemoryPlanDocuments() *MemoryPlanDocuments {
	return &MemoryPlanDocuments{data: make(map[string]string)}
}

func (d *MemoryPlanDocuments) Write(_ context.Context, sessionID, content string) (string, error) {
	if d == nil {
		return "", fmt.Errorf("plan documents: nil store")
	}
	if sessionID == "" {
		return "", fmt.Errorf("plan documents: empty session id")
	}
	rel := PlanDocumentRelPath(sessionID)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.data == nil {
		d.data = make(map[string]string)
	}
	d.data[sessionID] = content
	return rel, nil
}

func (d *MemoryPlanDocuments) Read(_ context.Context, sessionID string) (string, string, error) {
	if d == nil {
		return "", "", fmt.Errorf("plan documents: nil store")
	}
	rel := PlanDocumentRelPath(sessionID)
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.data == nil {
		return rel, "", nil
	}
	return rel, d.data[sessionID], nil
}
