package service

import (
	"context"
	"fmt"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// BackendResourceResolverRouter 只按可信 execution backend kind 与资源生命周期路由。
type BackendResourceResolverRouter struct {
	routes map[string]workcontract.BackendResourceResolver
}

func NewBackendResourceResolverRouter(routes []workcontract.BackendResourceResolverRoute) (*BackendResourceResolverRouter, error) {
	result := &BackendResourceResolverRouter{routes: make(map[string]workcontract.BackendResourceResolver, len(routes))}
	for _, route := range routes {
		if route.Resolver == nil || route.Backend == "" || (route.Availability != workmodel.ResourceAvailabilityDurable && route.Availability != workmodel.ResourceAvailabilityLeased) {
			return nil, fmt.Errorf("backend resource resolver route 无效")
		}
		key := backendResolverKey(string(route.Backend), string(route.Availability))
		if _, exists := result.routes[key]; exists {
			return nil, fmt.Errorf("backend resource resolver route 重复")
		}
		result.routes[key] = route.Resolver
	}
	return result, nil
}

func (r *BackendResourceResolverRouter) ResolveProducedResource(ctx context.Context, req workcontract.BackendResourceRequest) (workmodel.ResourceRef, error) {
	resolver, ok := r.routes[backendResolverKey(string(req.Execution.Backend.Kind), string(req.Availability))]
	if !ok {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeResourceReaderNotFound, fmt.Errorf("backend resource resolver 未注册: %s/%s", req.Execution.Backend.Kind, req.Availability))
	}
	return resolver.ResolveProducedResource(ctx, req)
}

func backendResolverKey(backend, availability string) string { return backend + "\x00" + availability }

var _ workcontract.BackendResourceResolver = (*BackendResourceResolverRouter)(nil)
