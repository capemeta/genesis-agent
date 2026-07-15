package stack

import (
	"context"
	"fmt"
	"strings"
	"time"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	auditcontract "genesis-agent/internal/capabilities/audit/contract"
	capcontract "genesis-agent/internal/capabilities/capability/contract"
	capservice "genesis-agent/internal/capabilities/capability/service"
	credentialcontract "genesis-agent/internal/capabilities/credential/contract"
	mcpapproval "genesis-agent/internal/capabilities/mcp/adapter/approval"
	mcpcapability "genesis-agent/internal/capabilities/mcp/adapter/capability"
	mcpcred "genesis-agent/internal/capabilities/mcp/adapter/credential"
	mcpobs "genesis-agent/internal/capabilities/mcp/adapter/observability"
	"genesis-agent/internal/capabilities/mcp/catalog"
	"genesis-agent/internal/capabilities/mcp/contract"
	mcpgateway "genesis-agent/internal/capabilities/mcp/gateway"
	"genesis-agent/internal/capabilities/mcp/manager"
	"genesis-agent/internal/capabilities/mcp/model"
	listmcpresources "genesis-agent/internal/capabilities/mcp/resourcetool/list_mcp_resources"
	mcpsearch "genesis-agent/internal/capabilities/mcp/resourcetool/mcp_search"
	readmcpresource "genesis-agent/internal/capabilities/mcp/resourcetool/read_mcp_resource"
	mcpscope "genesis-agent/internal/capabilities/mcp/scope"
	"genesis-agent/internal/capabilities/mcp/transport"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/gateway"
	tracecontract "genesis-agent/internal/capabilities/trace/contract"
	platformconfig "genesis-agent/internal/platform/config"
)

// Options 描述产品装配 MCP 栈的参数。
type Options struct {
	Config          *platformconfig.Config
	CapabilityIndex capcontract.Registry
	ToolRegistry    tool.Registry
	ApprovalService approvalcontract.Service
	CredentialSvc   credentialcontract.Service
	ApprovalStore   contract.ApprovalStore
	Requirements    contract.RequirementsFilter
	ExtraSources    []contract.DefinitionSource
	Channel         profilemodel.ChannelType
	Environment     profilemodel.RuntimeEnvironment
	TenantID        string
	ProjectID       string
	AgentID         string
	UserID          string
	RoleIDs         []string
	Workspace       string
	// EnableProjectSource 默认在 Workspace 非空时启用 `.genesis/mcp*`。
	EnableProjectSource *bool
	// ExistingAuthorizer 若非空，与 MCP Authorizer 组成链，避免覆盖。
	ExistingAuthorizer gateway.Authorizer
	// FailOnRequired 为 true 时，Required server 连接失败导致 Build 失败。
	FailOnRequired bool
	// AdapterRegistry 若非空则复用（便于与 marketplace 共享热更新）；否则新建。
	AdapterRegistry capcontract.RuntimeAdapterRegistry
	AuditSink       auditcontract.Sink
	Tracer          tracecontract.Tracer
}

// Stack 是已装配的 MCP 运行时组件。
type Stack struct {
	Manager         contract.Manager
	Catalog         contract.Catalog
	Gateway         *mcpgateway.Gateway
	Authorizer      gateway.Authorizer
	RuntimeAdapter  *mcpcapability.Adapter
	AdapterRegistry capcontract.RuntimeAdapterRegistry
	ApprovalStore   contract.ApprovalStore
	Tools           []tool.Tool
	Env             contract.RuntimeCatalogEnv
}

// Build 组装 MCP 内核并 Sync 一次。
func Build(ctx context.Context, opts Options) (*Stack, error) {
	if opts.Config == nil || !opts.Config.MCP.Enabled {
		return &Stack{}, nil
	}
	if opts.ToolRegistry == nil {
		return nil, fmt.Errorf("mcp stack 需要 ToolRegistry")
	}
	env := contract.RuntimeCatalogEnv{
		Channel:     opts.Channel,
		TenantID:    opts.TenantID,
		ProjectID:   opts.ProjectID,
		AgentID:     opts.AgentID,
		UserID:      opts.UserID,
		RoleIDs:     append([]string(nil), opts.RoleIDs...),
		Environment: opts.Environment,
		Workspace:   opts.Workspace,
	}
	creds := mcpcred.New(opts.CredentialSvc, opts.TenantID)
	factory := transport.NewFactory(creds, nil)
	mgr, err := manager.New(manager.Options{
		Factory:          factory,
		ConnectBatchSize: opts.Config.MCP.ConnectBatchSize,
		ApprovalStore:    opts.ApprovalStore,
	})
	if err != nil {
		return nil, err
	}

	sources := []contract.DefinitionSource{
		catalog.NewConfigSource(opts.Config),
	}
	if enableProjectSource(opts) {
		sources = append(sources, catalog.NewProjectSource(opts.Workspace))
	}
	if opts.CapabilityIndex != nil {
		sources = append(sources, catalog.NewMarketplaceSource(opts.CapabilityIndex))
	}
	sources = append(sources, opts.ExtraSources...)
	cat := catalog.New(sources, opts.Requirements)

	defs, err := cat.Merge(ctx, env)
	if err != nil {
		_ = mgr.Close(ctx)
		return nil, err
	}
	for i := range defs {
		if !mcpscope.Allows(defs[i].Config.Scope, env) {
			defs[i].Config.Enabled = false
			if defs[i].DisabledReason == "" {
				defs[i].DisabledReason = "capability scope 不匹配当前运行时"
			}
		}
	}

	gw := mcpgateway.New(mcpgateway.Options{
		Manager:  mgr,
		Registry: opts.ToolRegistry,
		ScopeEnv: env,
	})
	gw.SetDefinitions(defs)

	if opts.AuditSink != nil || opts.Tracer != nil {
		mgr.Subscribe(&mcpobs.Listener{Audit: opts.AuditSink, Tracer: opts.Tracer})
	}

	mcpAuth := mcpapproval.New(opts.ApprovalService)
	mcpAuth.SetDefinitions(defs)
	authz := gateway.NewChainAuthorizer(opts.ExistingAuthorizer, mcpAuth)

	runtimeAdapter := mcpcapability.New(mgr, cat, env)
	runtimeAdapter.OnDefinitions = func(next []model.McpServerDefinition) {
		gw.SetDefinitions(next)
		mcpAuth.SetDefinitions(next)
	}
	adapterReg := opts.AdapterRegistry
	if adapterReg == nil {
		adapterReg = capservice.NewAdapterRegistry()
	}
	if err := adapterReg.RegisterAdapter(runtimeAdapter); err != nil {
		_ = mgr.Close(ctx)
		return nil, fmt.Errorf("注册 MCP RuntimeAdapter 失败: %w", err)
	}

	// 默认 background：启动只应用 catalog，连接在后台进行，避免 npx 等慢启动堵死产品入口。
	connectMode := strings.ToLower(strings.TrimSpace(opts.Config.MCP.ConnectMode))
	if connectMode == "eager" {
		if _, err := mgr.Sync(ctx, defs); err != nil {
			_ = mgr.Close(ctx)
			return nil, err
		}
	} else {
		if _, err := mgr.SyncAsync(ctx, defs); err != nil {
			_ = mgr.Close(ctx)
			return nil, err
		}
	}
	gw.SyncFromStates(ctx)

	if opts.FailOnRequired {
		waitTimeout := opts.Config.MCP.DefaultStartupTimeout
		if waitTimeout <= 0 {
			waitTimeout = 30 * time.Second
		}
		waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
		err := mgr.WaitRequired(waitCtx)
		cancel()
		if err != nil {
			_ = mgr.Close(ctx)
			return nil, err
		}
	}

	tools := []tool.Tool{
		listmcpresources.New(mgr),
		readmcpresource.New(mgr),
		mcpsearch.New(opts.ToolRegistry),
	}
	for _, t := range tools {
		opts.ToolRegistry.Register(t)
	}

	return &Stack{
		Manager:         mgr,
		Catalog:         cat,
		Gateway:         gw,
		Authorizer:      authz,
		RuntimeAdapter:  runtimeAdapter,
		AdapterRegistry: adapterReg,
		ApprovalStore:   opts.ApprovalStore,
		Tools:           tools,
		Env:             env,
	}, nil
}

// Refresh 重新合并 Catalog 并 Sync（管理面 refresh / marketplace 热更新后可用）。
func (s *Stack) Refresh(ctx context.Context) error {
	if s == nil || s.Manager == nil || s.Catalog == nil {
		return fmt.Errorf("mcp stack 未初始化")
	}
	defs, err := s.Catalog.Merge(ctx, s.Env)
	if err != nil {
		return err
	}
	for i := range defs {
		if !mcpscope.Allows(defs[i].Config.Scope, s.Env) {
			defs[i].Config.Enabled = false
			if defs[i].DisabledReason == "" {
				defs[i].DisabledReason = "capability scope 不匹配当前运行时"
			}
		}
	}
	if s.RuntimeAdapter != nil && s.RuntimeAdapter.OnDefinitions != nil {
		s.RuntimeAdapter.OnDefinitions(defs)
	} else if s.Gateway != nil {
		s.Gateway.SetDefinitions(defs)
	}
	if _, err := s.Manager.Sync(ctx, defs); err != nil {
		return err
	}
	if s.Gateway != nil {
		s.Gateway.SyncFromStates(ctx)
	}
	return nil
}

// Close 释放连接与健康检查协程。
func (s *Stack) Close(ctx context.Context) error {
	if s == nil || s.Manager == nil {
		return nil
	}
	return s.Manager.Close(ctx)
}

func enableProjectSource(opts Options) bool {
	if opts.EnableProjectSource != nil {
		return *opts.EnableProjectSource && opts.Workspace != ""
	}
	return opts.Workspace != ""
}
