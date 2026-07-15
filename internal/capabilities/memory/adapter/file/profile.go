package file

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"genesis-agent/internal/domain"
)

// FileUserProfileStore 本地用户画像文件存储实现
type FileUserProfileStore struct {
	mu      sync.RWMutex
	baseDir string // 存储根目录，画像文件保存在 user_profiles 目录下
}

// NewFileUserProfileStore 创建本地用户画像存储实例
func NewFileUserProfileStore(baseDir string) *FileUserProfileStore {
	return &FileUserProfileStore{
		baseDir: baseDir,
	}
}

// profileFilePath 根据 tenantID 与 userID 拼接出画像文件路径
func (p *FileUserProfileStore) profileFilePath(tenantID, userID string) string {
	return filepath.Join(p.baseDir, "user_profiles", fmt.Sprintf("%s_%s.json", tenantID, userID))
}

// Get 获取用户画像。如果文件不存在，则返回一个初始化后的全新对象（不报错）
func (p *FileUserProfileStore) Get(ctx context.Context, tenantID, userID string) (*domain.UserProfile, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	filePath := p.profileFilePath(tenantID, userID)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// 返回带有默认内置画像的全新对象
		return &domain.UserProfile{
			TenantID: tenantID,
			UserID:   userID,
			Builtin: domain.UserProfileBuiltin{
				MemoryOptIn: true, // 默认开启跨会话长期记忆
			},
			CustomFields: make(map[string]any),
			Evidence:     make(map[string]domain.FieldEvidence),
			Confidence:   make(map[string]float64),
			Visibility:   make(map[string]string),
		}, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read user profile failed: %w", err)
	}

	var profile domain.UserProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("unmarshal user profile failed: %w", err)
	}

	return &profile, nil
}

// Save 保存用户画像，Builtin 与 CustomFields 字段统一合并写盘
func (p *FileUserProfileStore) Save(ctx context.Context, tenantID, userID string, profile *domain.UserProfile) error {
	if profile == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	filePath := p.profileFilePath(tenantID, userID)
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create user profile dir failed: %w", err)
	}

	profile.TenantID = tenantID
	profile.UserID = userID
	profile.UpdatedAt = time.Now()
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = time.Now()
	}

	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal user profile failed: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("write user profile failed: %w", err)
	}

	return nil
}
