// Package service 提供通用 Capability Registry 查询与运行时适配编排。
package service

import (
	"context"
	"fmt"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"sort"
	"strings"
	"sync"

	capcontract "genesis-agent/internal/capabilities/capability/contract"
)

type Registry struct {
	store    capcontract.RegistryStore
	adapters capcontract.RuntimeAdapterRegistry
}

type Options struct {
	Store    capcontract.RegistryStore
	Adapters capcontract.RuntimeAdapterRegistry
}

func NewRegistry(opts Options) (*Registry, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("capability registry store不能为空")
	}
	return &Registry{store: opts.Store, adapters: opts.Adapters}, nil
}

func (r *Registry) ListCapabilities(ctx context.Context, query capmodel.CapabilityQuery) ([]capmodel.CapabilityIndexRecord, error) {
	records, err := r.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]capmodel.CapabilityIndexRecord, 0, len(records))
	for _, record := range records {
		if !Matches(record, query) {
			continue
		}
		out = append(out, record)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *Registry) SetCapabilityEnabled(ctx context.Context, id string, enabled bool) (capmodel.CapabilityIndexRecord, error) {
	trimmed := strings.TrimSpace(id)
	record, ok, err := r.store.SetCapabilityEnabled(ctx, trimmed, enabled)
	if err != nil {
		return capmodel.CapabilityIndexRecord{}, err
	}
	if !ok {
		return capmodel.CapabilityIndexRecord{}, fmt.Errorf("capability %q 不存在", id)
	}
	if r.adapters != nil {
		if adapter, ok := r.adapters.AdapterFor(record.Type); ok {
			if err := adapter.SetEnabled(ctx, record, enabled); err != nil {
				_, _, _ = r.store.SetCapabilityEnabled(ctx, trimmed, !enabled)
				return capmodel.CapabilityIndexRecord{}, err
			}
		}
	}
	return record, nil
}

func Matches(record capmodel.CapabilityIndexRecord, query capmodel.CapabilityQuery) bool {
	if !query.IncludeDisabled && !record.Enabled {
		return false
	}
	if query.Scope != "" && record.Scope != query.Scope {
		return false
	}
	if len(query.Types) > 0 && !containsCapabilityType(query.Types, record.Type) {
		return false
	}
	if len(query.PackageTypes) > 0 && !containsPackageType(query.PackageTypes, record.PackageType) {
		return false
	}
	if query.Product != "" && len(record.Products) > 0 && !containsString(record.Products, query.Product) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(query.Query))
	if text == "" {
		return true
	}
	haystack := strings.ToLower(record.ID + " " + record.Name + " " + record.Description + " " + record.Package + " " + record.Marketplace + " " + record.ResourcePath + " " + string(record.Type) + " " + string(record.PackageType))
	return strings.Contains(haystack, text)
}

type AdapterRegistry struct {
	mu       sync.RWMutex
	adapters map[capmodel.CapabilityType]capcontract.RuntimeAdapter
}

func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{adapters: map[capmodel.CapabilityType]capcontract.RuntimeAdapter{}}
}

func (r *AdapterRegistry) RegisterAdapter(adapter capcontract.RuntimeAdapter) error {
	if adapter == nil {
		return fmt.Errorf("runtime adapter不能为空")
	}
	typ := adapter.CapabilityType()
	if typ == "" {
		return fmt.Errorf("runtime adapter capability type不能为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[typ] = adapter
	return nil
}

func (r *AdapterRegistry) AdapterFor(typ capmodel.CapabilityType) (capcontract.RuntimeAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[typ]
	return adapter, ok
}

func containsCapabilityType(items []capmodel.CapabilityType, value capmodel.CapabilityType) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func containsPackageType(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
