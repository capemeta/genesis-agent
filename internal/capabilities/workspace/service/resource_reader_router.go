package service

import (
	"context"
	"fmt"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// ResourceReaderRouter routes only by exact persisted authority and scheme.
type ResourceReaderRouter struct {
	routes map[string]workcontract.ResourceReader
}

func NewResourceReaderRouter(routes []workcontract.ResourceReaderRoute) (*ResourceReaderRouter, error) {
	router := &ResourceReaderRouter{routes: make(map[string]workcontract.ResourceReader, len(routes))}
	for _, route := range routes {
		authority, scheme := strings.TrimSpace(route.Authority), strings.TrimSpace(route.Scheme)
		if authority == "" || scheme == "" || authority != route.Authority || scheme != route.Scheme || route.Reader == nil || strings.ContainsAny(authority+scheme, "\\/\x00") {
			return nil, fmt.Errorf("resource reader route 无效")
		}
		key := readerRouteKey(authority, scheme)
		if _, exists := router.routes[key]; exists {
			return nil, fmt.Errorf("resource reader route %s/%s 重复", authority, scheme)
		}
		router.routes[key] = route.Reader
	}
	return router, nil
}

func (r *ResourceReaderRouter) Open(ctx context.Context, backend execmodel.ExecutionBackendRef, source workmodel.ResourceRef) (workcontract.ResourceHandle, error) {
	if err := backend.Validate(); err != nil {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeProducedResourceBackendMismatch, err)
	}
	if err := validateRouterSource(source); err != nil {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	if source.Authority != backend.Authority {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeProducedResourceBackendMismatch, fmt.Errorf("source authority 与 backend authority 不一致"))
	}
	reader, ok := r.routes[readerRouteKey(source.Authority, source.Scheme)]
	if !ok {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeResourceReaderNotFound, fmt.Errorf("resource reader %s/%s 未注册", source.Authority, source.Scheme))
	}
	handle, err := reader.Open(ctx, source)
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	if handle.Reader == nil {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("resource reader 返回空 reader"))
	}
	if handle.Version == "" || handle.Version != source.Version {
		_ = handle.Reader.Close()
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("resource version 不一致"))
	}
	return handle, nil
}

func validateRouterSource(source workmodel.ResourceRef) error {
	if strings.TrimSpace(source.Authority) == "" || source.Authority != strings.TrimSpace(source.Authority) ||
		strings.TrimSpace(source.Scheme) == "" || source.Scheme != strings.TrimSpace(source.Scheme) ||
		strings.TrimSpace(source.ID) == "" || source.ID != strings.TrimSpace(source.ID) ||
		strings.TrimSpace(source.Version) == "" || source.Version != strings.TrimSpace(source.Version) {
		return fmt.Errorf("source 缺少规范化 authority/scheme/id/version")
	}
	return nil
}

func readerRouteKey(authority, scheme string) string { return authority + "\x00" + scheme }
