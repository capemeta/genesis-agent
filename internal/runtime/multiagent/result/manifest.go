package result

import (
	"context"
	"sync"

	"genesis-agent/internal/runtime/multiagent/model"
)

type manifestContextKey struct{}

// ManifestRegistry 收集子 Run 显式登记的结构化结论和候选产物。
// 它不负责校验资源存在性，也不向父线程直接暴露内容。
type ManifestRegistry struct {
	mu        sync.RWMutex
	artifacts []model.Artifact
	findings  []model.Finding
}

// NewManifestRegistry 创建单个子 Run 独占的登记器。
func NewManifestRegistry() *ManifestRegistry { return &ManifestRegistry{} }

// WithManifestRegistry 将登记器绑定到子 Run 上下文。
func WithManifestRegistry(ctx context.Context, registry *ManifestRegistry) context.Context {
	if registry == nil {
		return ctx
	}
	return context.WithValue(ctx, manifestContextKey{}, registry)
}

// RegisterArtifact 登记候选产物。未绑定登记器时返回 false，调用方不得将其视为已交付。
func RegisterArtifact(ctx context.Context, artifact model.Artifact) bool {
	registry, ok := ctx.Value(manifestContextKey{}).(*ManifestRegistry)
	if !ok || registry == nil {
		return false
	}
	registry.RegisterArtifact(artifact)
	return true
}

// RegisterFinding 登记候选结构化结论。实际回传前仍需校验证据。
func RegisterFinding(ctx context.Context, finding model.Finding) bool {
	registry, ok := ctx.Value(manifestContextKey{}).(*ManifestRegistry)
	if !ok || registry == nil {
		return false
	}
	registry.RegisterFinding(finding)
	return true
}

// RegisterArtifact 向当前 Run 的候选清单追加一个产物副本。
func (r *ManifestRegistry) RegisterArtifact(artifact model.Artifact) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.artifacts = append(r.artifacts, artifact)
}

// RegisterFinding 向当前 Run 的候选清单追加一个结论副本。
func (r *ManifestRegistry) RegisterFinding(finding model.Finding) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.findings = append(r.findings, model.Finding{Claim: finding.Claim, Evidence: append([]string(nil), finding.Evidence...)})
}

// Snapshot 返回不可共享的终态候选快照。
func (r *ManifestRegistry) Snapshot() (model.ArtifactManifest, []model.Finding) {
	if r == nil {
		return model.ArtifactManifest{}, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	manifest := model.ArtifactManifest{Artifacts: append([]model.Artifact(nil), r.artifacts...)}
	findings := make([]model.Finding, 0, len(r.findings))
	for _, finding := range r.findings {
		findings = append(findings, model.Finding{Claim: finding.Claim, Evidence: append([]string(nil), finding.Evidence...)})
	}
	return manifest, findings
}
