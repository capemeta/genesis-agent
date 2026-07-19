// Package plan 提供本地隔离的文件存储实现。
package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"genesis-agent/internal/capabilities/tasklist/contract"
	"genesis-agent/internal/capabilities/tasklist/model"
)

var _ contract.Repository = (*FileRepository)(nil)

// FileRepository 本地 JSON 文件存储适配器
type FileRepository struct {
	mu      sync.RWMutex
	baseDir string
}

// NewFileRepository 创建 FileRepository 实例
func NewFileRepository(baseDir string) (*FileRepository, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("create file repository base dir failed: %w", err)
	}
	return &FileRepository{baseDir: baseDir}, nil
}

func (r *FileRepository) getPlanPath(sessionID string) string {
	return filepath.Join(r.baseDir, sessionIDSafe(sessionID)+"_plan.json")
}

func (r *FileRepository) getHistoryPath(sessionID string) string {
	return filepath.Join(r.baseDir, sessionIDSafe(sessionID)+"_history.json")
}

// GetPlan 获取指定 Session 的最新计划快照
func (r *FileRepository) GetTaskList(ctx context.Context, sessionID string) (*model.TaskList, error) {
	safeID := sessionIDSafe(sessionID)
	if safeID == "" {
		return nil, fmt.Errorf("invalid session id: %q", sessionID)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	path := r.getPlanPath(sessionID)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("read plan file failed: %w", err)
	}

	var plan model.TaskList
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan failed: %w", err)
	}
	return &plan, nil
}

// SavePlan 保存最新计划，且追加审计变更记录（解耦读写膨胀风险）
func (r *FileRepository) SaveTaskList(ctx context.Context, plan *model.TaskList, revision *model.RevisionLog) error {
	safeID := sessionIDSafe(plan.SessionID)
	if safeID == "" {
		return fmt.Errorf("invalid session id: %q", plan.SessionID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// 1. 读取并校验并发版本 (乐观锁)
	planPath := r.getPlanPath(plan.SessionID)
	var existingVersion int64 = 0

	existingData, err := os.ReadFile(planPath)
	if err == nil {
		var existing model.TaskList
		if json.Unmarshal(existingData, &existing) == nil {
			existingVersion = existing.Version
		}
	}

	if plan.Version <= existingVersion {
		return fmt.Errorf("concurrency conflict: current version is %d, proposed version %d must be greater", existingVersion, plan.Version)
	}

	// 2. 写入主快照
	planData, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal plan snapshot failed: %w", err)
	}

	if err := os.WriteFile(planPath, planData, 0644); err != nil {
		return fmt.Errorf("write plan file failed: %w", err)
	}

	// 3. 追加变更历史
	historyPath := r.getHistoryPath(plan.SessionID)
	var history []model.RevisionLog

	historyData, err := os.ReadFile(historyPath)
	if err == nil {
		_ = json.Unmarshal(historyData, &history)
	}

	history = append(history, *revision)
	newHistoryData, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("marshal revision logs failed: %w", err)
	}

	if err := os.WriteFile(historyPath, newHistoryData, 0644); err != nil {
		return fmt.Errorf("write revision log file failed: %w", err)
	}

	return nil
}

// GetHistory 获取变更日志记录
func (r *FileRepository) GetHistory(ctx context.Context, sessionID string) ([]model.RevisionLog, error) {
	safeID := sessionIDSafe(sessionID)
	if safeID == "" {
		return nil, fmt.Errorf("invalid session id: %q", sessionID)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	historyPath := r.getHistoryPath(sessionID)
	data, err := os.ReadFile(historyPath)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("read revision history file failed: %w", err)
	}

	var history []model.RevisionLog
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("unmarshal revision history failed: %w", err)
	}
	return history, nil
}

var sessionIDRegex = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func sessionIDSafe(raw string) string {
	// 使用正则表达式仅保留字母、数字、破折号和下划线
	return sessionIDRegex.ReplaceAllString(raw, "")
}
