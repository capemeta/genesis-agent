package skillmarket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"os"
	"path/filepath"
	"sort"
	"sync"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

type RegistryStore struct {
	path string
	mu   sync.Mutex
}

type InstallStore struct {
	path string
	mu   sync.Mutex
}

type CapabilityIndexStore struct {
	path string
	mu   sync.Mutex
}

func NewRegistryStore(path string) *RegistryStore { return &RegistryStore{path: path} }
func NewInstallStore(path string) *InstallStore   { return &InstallStore{path: path} }
func NewCapabilityIndexStore(path string) *CapabilityIndexStore {
	return &CapabilityIndexStore{path: path}
}

func (s *RegistryStore) List(ctx context.Context) ([]marketmodel.MarketplaceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items, err := readRecordMap[marketmodel.MarketplaceRecord](s.path)
	if err != nil {
		return nil, err
	}
	out := make([]marketmodel.MarketplaceRecord, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *RegistryStore) Get(ctx context.Context, name string) (marketmodel.MarketplaceRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return marketmodel.MarketplaceRecord{}, false, err
	}
	items, err := readRecordMap[marketmodel.MarketplaceRecord](s.path)
	if err != nil {
		return marketmodel.MarketplaceRecord{}, false, err
	}
	item, ok := items[name]
	return item, ok, nil
}

func (s *RegistryStore) Put(ctx context.Context, record marketmodel.MarketplaceRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.Name == "" {
		return fmt.Errorf("marketplace name不能为空")
	}
	items, err := readRecordMap[marketmodel.MarketplaceRecord](s.path)
	if err != nil {
		return err
	}
	items[record.Name] = record
	return writeRecordMap(s.path, items)
}

func (s *RegistryStore) Delete(ctx context.Context, name string) (marketmodel.MarketplaceRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return marketmodel.MarketplaceRecord{}, false, err
	}
	items, err := readRecordMap[marketmodel.MarketplaceRecord](s.path)
	if err != nil {
		return marketmodel.MarketplaceRecord{}, false, err
	}
	item, ok := items[name]
	if !ok {
		return marketmodel.MarketplaceRecord{}, false, nil
	}
	delete(items, name)
	return item, true, writeRecordMap(s.path, items)
}

func (s *InstallStore) List(ctx context.Context) ([]marketmodel.InstallRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items, err := readRecordMap[marketmodel.InstallRecord](s.path)
	if err != nil {
		return nil, err
	}
	out := make([]marketmodel.InstallRecord, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Spec < out[j].Spec })
	return out, nil
}

func (s *InstallStore) Get(ctx context.Context, spec string) (marketmodel.InstallRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return marketmodel.InstallRecord{}, false, err
	}
	items, err := readRecordMap[marketmodel.InstallRecord](s.path)
	if err != nil {
		return marketmodel.InstallRecord{}, false, err
	}
	item, ok := items[spec]
	return item, ok, nil
}

func (s *InstallStore) Put(ctx context.Context, record marketmodel.InstallRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.Spec == "" {
		record.Spec = marketmodel.PackageSpec(record.Package, record.Marketplace)
	}
	items, err := readRecordMap[marketmodel.InstallRecord](s.path)
	if err != nil {
		return err
	}
	items[record.Spec] = record
	return writeRecordMap(s.path, items)
}

func (s *InstallStore) Delete(ctx context.Context, spec string) (marketmodel.InstallRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return marketmodel.InstallRecord{}, false, err
	}
	items, err := readRecordMap[marketmodel.InstallRecord](s.path)
	if err != nil {
		return marketmodel.InstallRecord{}, false, err
	}
	item, ok := items[spec]
	if !ok {
		return marketmodel.InstallRecord{}, false, nil
	}
	delete(items, spec)
	return item, true, writeRecordMap(s.path, items)
}

func (s *CapabilityIndexStore) List(ctx context.Context) ([]capmodel.CapabilityIndexRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items, err := readRecordMap[capmodel.CapabilityIndexRecord](s.path)
	if err != nil {
		return nil, err
	}
	out := make([]capmodel.CapabilityIndexRecord, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *CapabilityIndexStore) PutPackageCapabilities(ctx context.Context, spec string, records []capmodel.CapabilityIndexRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	items, err := readRecordMap[capmodel.CapabilityIndexRecord](s.path)
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
			return fmt.Errorf("capability index缺少id: %s", record.Name)
		}
		items[record.ID] = record
	}
	return writeRecordMap(s.path, items)
}

func (s *CapabilityIndexStore) SetPackageEnabled(ctx context.Context, spec string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	items, err := readRecordMap[capmodel.CapabilityIndexRecord](s.path)
	if err != nil {
		return err
	}
	for id, record := range items {
		if record.Spec == spec {
			record.Enabled = enabled
			items[id] = record
		}
	}
	return writeRecordMap(s.path, items)
}

func (s *CapabilityIndexStore) SetCapabilityEnabled(ctx context.Context, id string, enabled bool) (capmodel.CapabilityIndexRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return capmodel.CapabilityIndexRecord{}, false, err
	}
	items, err := readRecordMap[capmodel.CapabilityIndexRecord](s.path)
	if err != nil {
		return capmodel.CapabilityIndexRecord{}, false, err
	}
	record, ok := items[id]
	if !ok {
		return capmodel.CapabilityIndexRecord{}, false, nil
	}
	record.Enabled = enabled
	items[id] = record
	if err := writeRecordMap(s.path, items); err != nil {
		return capmodel.CapabilityIndexRecord{}, false, err
	}
	return record, true, nil
}
func (s *CapabilityIndexStore) DeletePackage(ctx context.Context, spec string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	items, err := readRecordMap[capmodel.CapabilityIndexRecord](s.path)
	if err != nil {
		return err
	}
	for id, record := range items {
		if record.Spec == spec {
			delete(items, id)
		}
	}
	return writeRecordMap(s.path, items)
}

func readRecordMap[T any](path string) (map[string]T, error) {
	items := map[string]T{}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return items, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return items, nil
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("解析%s失败: %w", path, err)
	}
	return items, nil
}

func writeRecordMap[T any](path string, items map[string]T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil {
		_ = os.Remove(tmpName)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpName)
		return closeErr
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
