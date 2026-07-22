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
)

// AdoptionRecord 是「消费者 Run（父）」对「生产者 Run（子）产物」的显式、版本锁定、可审计接纳记录。
//
// 它替代旧的「创建即全局可读」隐式索引：跨 Run 可读性只来自一次显式 Adopt，
// 而这次 Adopt 只发生在父子边界（Controller.finish），且只针对已过滤的交付候选（QA/中间物在归约层剔除）。
// 授权由父子派生关系保证（父 spawn 子、同 scope）；读取按资源所属 backend 完成。
type AdoptionRecord struct {
	ConsumerTenantID string    `json:"consumer_tenant_id"`
	ConsumerRunID    string    `json:"consumer_run_id"`
	ProducedID       string    `json:"produced_id"`
	OwnerTenantID    string    `json:"owner_tenant_id"`
	OwnerRunID       string    `json:"owner_run_id"`
	AgentID          string    `json:"agent_id,omitempty"`
	ContentHash      string    `json:"content_hash,omitempty"`
	Role             string    `json:"role,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// AdoptionStore 是 Harness 级、磁盘持久化的接纳记录存储（内存缓存 + 落盘），按 (consumer_run, produced_id) 排他。
type AdoptionStore struct {
	mu        sync.RWMutex
	items     map[string]AdoptionRecord
	stateRoot string
}

// GlobalAdoptionStore 是 Harness 级全局接纳存储实例。
var GlobalAdoptionStore = NewAdoptionStore()

// NewAdoptionStore 创建新的接纳存储。
func NewAdoptionStore() *AdoptionStore {
	return &AdoptionStore{items: make(map[string]AdoptionRecord)}
}

// SetStateRoot 配置接纳记录落盘根目录（如 .genesis）。
func (s *AdoptionStore) SetStateRoot(stateRoot string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if root, err := filepath.Abs(strings.TrimSpace(stateRoot)); err == nil && root != "" {
		s.stateRoot = root
	}
}

func adoptionMemKey(consumerRunID, producedID string) string {
	return strings.TrimSpace(consumerRunID) + "\x00" + strings.TrimSpace(producedID)
}

func adoptionFilename(stateRoot, consumerRunID, producedID string) string {
	if strings.TrimSpace(stateRoot) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(adoptionMemKey(consumerRunID, producedID)))
	return filepath.Join(stateRoot, "runtime", "adoptions", hex.EncodeToString(sum[:])+".json")
}

// Adopt 幂等写入接纳记录并做版本锁校验：
// 同 (consumer_run, produced_id) 已存在且内容哈希不同 → 版本冲突（防止子被重跑后父静默读到旧版本）；
// 相同则返回既有记录（幂等）。
func (s *AdoptionStore) Adopt(record AdoptionRecord) (AdoptionRecord, error) {
	record.ConsumerTenantID = strings.TrimSpace(record.ConsumerTenantID)
	record.ConsumerRunID = strings.TrimSpace(record.ConsumerRunID)
	record.ProducedID = strings.TrimSpace(record.ProducedID)
	record.OwnerTenantID = strings.TrimSpace(record.OwnerTenantID)
	record.OwnerRunID = strings.TrimSpace(record.OwnerRunID)
	if record.ConsumerRunID == "" || record.ProducedID == "" || record.OwnerRunID == "" {
		return AdoptionRecord{}, fmt.Errorf("adoption 记录缺少 consumer_run/produced_id/owner_run")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	memKey := adoptionMemKey(record.ConsumerRunID, record.ProducedID)
	s.mu.Lock()
	stateRoot := s.stateRoot
	existing, ok := s.items[memKey]
	if !ok {
		if loaded, found := readAdoptionRecord(adoptionFilename(stateRoot, record.ConsumerRunID, record.ProducedID)); found {
			existing, ok = loaded, true
		}
	}
	if ok {
		if existing.ContentHash != "" && record.ContentHash != "" && existing.ContentHash != record.ContentHash {
			s.mu.Unlock()
			return AdoptionRecord{}, fmt.Errorf("adoption 版本冲突: produced %q 在 Run %q 已接纳不同版本内容", record.ProducedID, record.ConsumerRunID)
		}
		s.items[memKey] = existing
		s.mu.Unlock()
		return existing, nil
	}
	s.items[memKey] = record
	filename := adoptionFilename(stateRoot, record.ConsumerRunID, record.ProducedID)
	s.mu.Unlock()

	if filename != "" {
		if data, err := json.Marshal(record); err == nil {
			_ = os.MkdirAll(filepath.Dir(filename), 0o700)
			_ = os.WriteFile(filename, data, 0o600)
		}
	}
	return record, nil
}

// Resolve 在消费者作用域解析被接纳产物的真实所属 Run；未接纳则返回 false（不再有「假跨 Run 可读」）。
// stateRoot 为可选覆盖：调用方可传 prepared.Manifest.StateRoot.Path，未传则用已配置根目录。
func (s *AdoptionStore) Resolve(consumerTenantID, consumerRunID, producedID, stateRoot string) (AdoptionRecord, bool) {
	consumerRunID = strings.TrimSpace(consumerRunID)
	producedID = strings.TrimSpace(producedID)
	if consumerRunID == "" || producedID == "" {
		return AdoptionRecord{}, false
	}
	memKey := adoptionMemKey(consumerRunID, producedID)
	s.mu.RLock()
	rec, ok := s.items[memKey]
	if strings.TrimSpace(stateRoot) == "" {
		stateRoot = s.stateRoot
	}
	s.mu.RUnlock()
	if ok {
		return rec, true
	}
	if loaded, found := readAdoptionRecord(adoptionFilename(stateRoot, consumerRunID, producedID)); found {
		s.mu.Lock()
		s.items[memKey] = loaded
		s.mu.Unlock()
		return loaded, true
	}
	return AdoptionRecord{}, false
}

// ListByConsumer 列出某消费者 Run 已接纳的全部记录（内存 + 落盘扫描）。
// 用于父完成门禁：以「已接纳且子已交付」销父侧 required deliverable。
func (s *AdoptionStore) ListByConsumer(consumerRunID string) []AdoptionRecord {
	consumerRunID = strings.TrimSpace(consumerRunID)
	if consumerRunID == "" {
		return nil
	}
	out := map[string]AdoptionRecord{}
	s.mu.RLock()
	stateRoot := s.stateRoot
	for _, rec := range s.items {
		if strings.TrimSpace(rec.ConsumerRunID) == consumerRunID {
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
				if !ok || strings.TrimSpace(loaded.ConsumerRunID) != consumerRunID {
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
	result := make([]AdoptionRecord, 0, len(out))
	for _, rec := range out {
		result = append(result, rec)
	}
	return result
}

func readAdoptionRecord(filename string) (AdoptionRecord, bool) {
	if filename == "" {
		return AdoptionRecord{}, false
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return AdoptionRecord{}, false
	}
	var rec AdoptionRecord
	if json.Unmarshal(data, &rec) != nil || strings.TrimSpace(rec.OwnerRunID) == "" {
		return AdoptionRecord{}, false
	}
	return rec, true
}
