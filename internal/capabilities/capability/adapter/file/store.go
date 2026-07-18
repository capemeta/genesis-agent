// Package file 提供 Capability Registry 的原子文件存储适配器。
package file

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	capmodel "genesis-agent/internal/capabilities/capability/model"
)

type Store struct {
	path string
	mu   sync.Mutex
}

func New(path string) *Store { return &Store{path: path} }

func (s *Store) List(ctx context.Context) ([]capmodel.CapabilityIndexRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items, err := s.read()
	if err != nil {
		return nil, err
	}
	out := make([]capmodel.CapabilityIndexRecord, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) SetCapabilityEnabled(ctx context.Context, id string, enabled bool) (capmodel.CapabilityIndexRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return capmodel.CapabilityIndexRecord{}, false, err
	}
	items, err := s.read()
	if err != nil {
		return capmodel.CapabilityIndexRecord{}, false, err
	}
	record, ok := items[id]
	if !ok {
		return capmodel.CapabilityIndexRecord{}, false, nil
	}
	record.Enabled = enabled
	items[id] = record
	return record, true, s.write(items)
}

func (s *Store) PutPackageCapabilities(ctx context.Context, spec string, records []capmodel.CapabilityIndexRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	items, err := s.read()
	if err != nil {
		return err
	}
	for id, record := range items {
		if record.Spec == spec {
			delete(items, id)
		}
	}
	for _, record := range records {
		if record.ID == "" {
			return fmt.Errorf("capability index 缺少 id")
		}
		items[record.ID] = record
	}
	return s.write(items)
}

func (s *Store) SetPackageEnabled(ctx context.Context, spec string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	items, err := s.read()
	if err != nil {
		return err
	}
	for id, record := range items {
		if record.Spec == spec {
			record.Enabled = enabled
			items[id] = record
		}
	}
	return s.write(items)
}

func (s *Store) DeletePackage(ctx context.Context, spec string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	items, err := s.read()
	if err != nil {
		return err
	}
	for id, record := range items {
		if record.Spec == spec {
			delete(items, id)
		}
	}
	return s.write(items)
}

func (s *Store) read() (map[string]capmodel.CapabilityIndexRecord, error) {
	items := map[string]capmodel.CapabilityIndexRecord{}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, fmt.Errorf("解析 capability index 失败: %w", err)
		}
	}
	return items, nil
}

func (s *Store) write(items map[string]capmodel.CapabilityIndexRecord) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".capability-index-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	committed = true
	return nil
}
