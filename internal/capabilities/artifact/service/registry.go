package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
)

// AdoptionStore 是 Harness 级、磁盘持久化的接纳记录存储（内存缓存 + 落盘），按 (consumer_run, produced_id) 排他。
type AdoptionStore struct {
	mu        sync.RWMutex
	items     map[string]artifactcontract.AdoptionRecord
	stateRoot string
}

// NewAdoptionStore 创建绑定到单一租户/工作空间 state root 的接纳存储。
func NewAdoptionStore(stateRoot string) (*AdoptionStore, error) {
	stateRoot = strings.TrimSpace(stateRoot)
	if stateRoot == "" {
		return nil, fmt.Errorf("adoption state root不能为空")
	}
	root, err := filepath.Abs(stateRoot)
	if err != nil || root == "" {
		return nil, fmt.Errorf("解析 adoption state root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "runtime", "adoptions"), 0o700); err != nil {
		return nil, fmt.Errorf("创建 adoption state root: %w", err)
	}
	return &AdoptionStore{items: make(map[string]artifactcontract.AdoptionRecord), stateRoot: root}, nil
}

func adoptionMemKey(consumerTenantID, consumerRunID, producedID string) string {
	return strings.TrimSpace(consumerTenantID) + "\x00" + strings.TrimSpace(consumerRunID) + "\x00" + strings.TrimSpace(producedID)
}

func adoptionFilename(stateRoot, consumerTenantID, consumerRunID, producedID string) string {
	if strings.TrimSpace(stateRoot) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(adoptionMemKey(consumerTenantID, consumerRunID, producedID)))
	return filepath.Join(stateRoot, "runtime", "adoptions", hex.EncodeToString(sum[:])+".json")
}

// Adopt 幂等写入接纳记录并做版本锁校验：
// 同 (consumer_run, produced_id) 已存在且内容哈希不同 → 版本冲突（防止子被重跑后父静默读到旧版本）；
// 相同则返回既有记录（幂等）。
func (s *AdoptionStore) Adopt(record artifactcontract.AdoptionRecord) (artifactcontract.AdoptionRecord, error) {
	record.ConsumerTenantID = strings.TrimSpace(record.ConsumerTenantID)
	record.ConsumerRunID = strings.TrimSpace(record.ConsumerRunID)
	record.ProducedID = strings.TrimSpace(record.ProducedID)
	record.OwnerTenantID = strings.TrimSpace(record.OwnerTenantID)
	record.OwnerRunID = strings.TrimSpace(record.OwnerRunID)
	if record.ConsumerTenantID == "" || record.ConsumerRunID == "" || record.ProducedID == "" || record.OwnerTenantID == "" || record.OwnerRunID == "" || strings.TrimSpace(record.ContentHash) == "" {
		return artifactcontract.AdoptionRecord{}, fmt.Errorf("adoption 记录缺少 consumer_tenant/consumer_run/produced_id/owner_tenant/owner_run/content_hash")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	memKey := adoptionMemKey(record.ConsumerTenantID, record.ConsumerRunID, record.ProducedID)
	s.mu.Lock()
	defer s.mu.Unlock()
	stateRoot := s.stateRoot
	existing, ok := s.items[memKey]
	if !ok {
		if loaded, found := readAdoptionRecord(adoptionFilename(stateRoot, record.ConsumerTenantID, record.ConsumerRunID, record.ProducedID)); found {
			existing, ok = loaded, true
		}
	}
	if ok {
		if existing.ConsumerTenantID != record.ConsumerTenantID || existing.OwnerTenantID != record.OwnerTenantID || existing.OwnerRunID != record.OwnerRunID || existing.ContentHash != record.ContentHash {
			return artifactcontract.AdoptionRecord{}, fmt.Errorf("adoption 版本冲突: produced %q 在 Run %q 已接纳不同版本内容", record.ProducedID, record.ConsumerRunID)
		}
		s.items[memKey] = existing
		return existing, nil
	}
	filename := adoptionFilename(stateRoot, record.ConsumerTenantID, record.ConsumerRunID, record.ProducedID)
	if filename != "" {
		if err := writeAdoptionRecord(filename, record); err != nil {
			return artifactcontract.AdoptionRecord{}, err
		}
	}
	s.items[memKey] = record
	return record, nil
}

// Resolve 在消费者作用域解析被接纳产物的真实所属 Run；未接纳则返回 false（不再有「假跨 Run 可读」）。
// stateRoot 为可选覆盖：调用方可传 prepared.Manifest.StateRoot.Path，未传则用已配置根目录。
func (s *AdoptionStore) Resolve(consumerTenantID, consumerRunID, producedID string) (artifactcontract.AdoptionRecord, bool) {
	consumerRunID = strings.TrimSpace(consumerRunID)
	producedID = strings.TrimSpace(producedID)
	if consumerRunID == "" || producedID == "" {
		return artifactcontract.AdoptionRecord{}, false
	}
	consumerTenantID = strings.TrimSpace(consumerTenantID)
	memKey := adoptionMemKey(consumerTenantID, consumerRunID, producedID)
	s.mu.RLock()
	rec, ok := s.items[memKey]
	stateRoot := s.stateRoot
	s.mu.RUnlock()
	if ok && rec.ConsumerTenantID == consumerTenantID {
		return rec, true
	}
	if loaded, found := readAdoptionRecord(adoptionFilename(stateRoot, consumerTenantID, consumerRunID, producedID)); found && loaded.ConsumerTenantID == consumerTenantID {
		s.mu.Lock()
		s.items[memKey] = loaded
		s.mu.Unlock()
		return loaded, true
	}
	return artifactcontract.AdoptionRecord{}, false
}

// ListByConsumer 列出某消费者 Run 已接纳的全部记录（内存 + 落盘扫描）。
// 用于父完成门禁：以「已接纳且子已交付」销父侧 required deliverable。
func (s *AdoptionStore) ListByConsumer(consumerTenantID, consumerRunID string) []artifactcontract.AdoptionRecord {
	consumerTenantID = strings.TrimSpace(consumerTenantID)
	consumerRunID = strings.TrimSpace(consumerRunID)
	if consumerRunID == "" {
		return nil
	}
	out := map[string]artifactcontract.AdoptionRecord{}
	s.mu.RLock()
	stateRoot := s.stateRoot
	for _, rec := range s.items {
		if strings.TrimSpace(rec.ConsumerTenantID) == consumerTenantID && strings.TrimSpace(rec.ConsumerRunID) == consumerRunID {
			out[rec.ProducedID] = rec
		}
	}
	s.mu.RUnlock()
	if stateRoot != "" {
		dir := filepath.Join(stateRoot, "runtime", "adoptions")
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
					continue
				}
				loaded, ok := readAdoptionRecord(filepath.Join(dir, entry.Name()))
				if !ok || strings.TrimSpace(loaded.ConsumerTenantID) != consumerTenantID || strings.TrimSpace(loaded.ConsumerRunID) != consumerRunID {
					continue
				}
				if _, exists := out[loaded.ProducedID]; !exists {
					out[loaded.ProducedID] = loaded
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	result := make([]artifactcontract.AdoptionRecord, 0, len(out))
	for _, rec := range out {
		result = append(result, rec)
	}
	return result
}

func readAdoptionRecord(filename string) (artifactcontract.AdoptionRecord, bool) {
	if filename == "" {
		return artifactcontract.AdoptionRecord{}, false
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return artifactcontract.AdoptionRecord{}, false
	}
	var rec artifactcontract.AdoptionRecord
	if json.Unmarshal(data, &rec) != nil || strings.TrimSpace(rec.ConsumerTenantID) == "" || strings.TrimSpace(rec.OwnerRunID) == "" || strings.TrimSpace(rec.ContentHash) == "" {
		return artifactcontract.AdoptionRecord{}, false
	}
	return rec, true
}

func writeAdoptionRecord(filename string, record artifactcontract.AdoptionRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("编码 adoption record: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return fmt.Errorf("创建 adoption 目录: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(filename), ".adoption-*.tmp")
	if err != nil {
		return fmt.Errorf("创建 adoption 临时文件: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("写入 adoption record: %w", err)
	}
	if err := os.Rename(tmpName, filename); err != nil {
		return fmt.Errorf("提交 adoption record: %w", err)
	}
	return nil
}

var _ artifactcontract.AdoptionStore = (*AdoptionStore)(nil)
