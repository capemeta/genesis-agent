// Package controlplane 提供 Enterprise 租户级 Artifact 控制面显式工厂。
package controlplane

import (
	"fmt"
	"strings"

	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	workspaceadapter "genesis-agent/internal/capabilities/workspace/adapter/sandbox"
	enterprisebootstrap "genesis-agent/products/enterprise/bootstrap"
	localartifactcontrol "genesis-agent/shared/local/artifactcontrol"
	localskill "genesis-agent/shared/local/skill"
	localsubagent "genesis-agent/shared/local/subagent"
	localworkspace "genesis-agent/shared/local/workspace"
)

// Options 描述租户级控制面装配参数；StateRoot 必须由部署层注入，Init 不会自动回退。
type Options struct {
	TenantStateRoot        string
	DeliveryRoot           string
	FileClient             sandboxcontract.FileSystemClient
	ArtifactDownloader     workspaceadapter.ArtifactByteDownloader
	BufferedObjectMaxBytes int64
}

// BuildTenantDependencies 从租户 StateRoot 显式装配 Enterprise Dependencies。
func BuildTenantDependencies(opts Options) (enterprisebootstrap.Dependencies, error) {
	if strings.TrimSpace(opts.TenantStateRoot) == "" {
		return enterprisebootstrap.Dependencies{}, fmt.Errorf("Enterprise 租户 StateRoot 未配置")
	}
	manifests, err := localworkspace.NewManifestStore(opts.TenantStateRoot)
	if err != nil {
		return enterprisebootstrap.Dependencies{}, fmt.Errorf("创建租户 RunManifestStore 失败: %w", err)
	}
	control, err := localartifactcontrol.Build(localartifactcontrol.Options{
		StateRoot:              opts.TenantStateRoot,
		DeliveryWorkspaceRoot:  opts.DeliveryRoot,
		FileClient:             opts.FileClient,
		ArtifactDownloader:     opts.ArtifactDownloader,
		BufferedObjectMaxBytes: opts.BufferedObjectMaxBytes,
	})
	if err != nil {
		return enterprisebootstrap.Dependencies{}, fmt.Errorf("装配租户 Artifact 控制面失败: %w", err)
	}
	bindingStore, err := localskill.NewBindingStore(opts.TenantStateRoot)
	if err != nil {
		return enterprisebootstrap.Dependencies{}, fmt.Errorf("装配租户 InvocationBindingStore 失败: %w", err)
	}
	packageStore, err := localskill.NewPackageStore(opts.TenantStateRoot)
	if err != nil {
		return enterprisebootstrap.Dependencies{}, fmt.Errorf("装配租户 SkillPackageSnapshotStore 失败: %w", err)
	}
	subAgentStore, err := localsubagent.NewStore(opts.TenantStateRoot)
	if err != nil {
		return enterprisebootstrap.Dependencies{}, fmt.Errorf("装配租户 SubAgent InstanceStore 失败: %w", err)
	}
	return enterprisebootstrap.Dependencies{
		RunManifests:      manifests,
		ProducedResources: control.Produced,
		ProducedStore:     control.ProducedStore,
		ResourceReaders:   control.Readers,
		RemoteSessions:    control.RemoteSessions,
		Reservations:      control.Reservations,
		Deliverables:      control.Deliverables,
		ArtifactRuns:      control.Initializer,
		Finalizer:         control.Finalizer,
		Completion:        control.Completion,
		QAEvidence:        control.QAEvidence,
		Adoptions:         control.Adoptions,
		SkillBindings:     bindingStore,
		SkillPackages:     packageStore,
		SubAgentStore:     subAgentStore,
	}, nil
}
