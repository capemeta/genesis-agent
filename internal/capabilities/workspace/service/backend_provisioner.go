package service

import (
	"context"
	"fmt"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

// BackendProvisioner 按 Harness 已固化的 backend kind 选择物理适配器。
// 路由只读取可信 PrepareRequest.Backend，不根据 provider 名猜测。
type BackendProvisioner struct {
	host         workcontract.Provisioner
	localSandbox workcontract.Provisioner
	remote       workcontract.Provisioner
}

func NewBackendProvisioner(host, localSandbox, remote workcontract.Provisioner) (*BackendProvisioner, error) {
	if host == nil || localSandbox == nil || remote == nil {
		return nil, fmt.Errorf("backend provisioner 缺少 host/local-sandbox/remote adapter")
	}
	return &BackendProvisioner{host: host, localSandbox: localSandbox, remote: remote}, nil
}

func (p *BackendProvisioner) Prepare(ctx context.Context, req workcontract.PrepareRequest) (workcontract.PreparedExecution, error) {
	backend := req.Backend
	if backend.Kind == "" {
		backend = execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindHost, Provider: "local-host", Authority: "host"}
		req.Backend = backend
	}
	if err := backend.Validate(); err != nil {
		return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	var provisioner workcontract.Provisioner
	switch backend.Kind {
	case execmodel.BackendKindHost:
		provisioner = p.host
	case execmodel.BackendKindLocalSandbox:
		provisioner = p.localSandbox
	case execmodel.BackendKindRemote:
		provisioner = p.remote
	default:
		return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("不支持的 backend kind %q", backend.Kind))
	}
	prepared, err := provisioner.Prepare(ctx, req)
	if err != nil {
		return workcontract.PreparedExecution{}, err
	}
	if prepared.Backend != backend {
		return workcontract.PreparedExecution{}, workcontract.NewError(workcontract.ErrCodeResourceBackendMismatch, fmt.Errorf("provisioner 改写了不可变 backend"))
	}
	return prepared, nil
}

var _ workcontract.Provisioner = (*BackendProvisioner)(nil)
