package result

import (
	"context"

	"genesis-agent/internal/runtime/multiagent/model"
)

// ValidatedEvidence 是 EvidenceValidator 允许进入规范化结果的最小集合。
type ValidatedEvidence struct {
	Artifacts []model.Artifact
	Findings  []model.Finding
}

// EvidenceValidator 验证候选产物的存在性、路径和证据引用。
// 实现可访问受控资源后端；Reducer 本身绝不读取文件系统或扫描工作区。
type EvidenceValidator interface {
	Validate(ctx context.Context, manifest model.ArtifactManifest, findings []model.Finding) (ValidatedEvidence, error)
}

// PassthroughEvidenceValidator 允许候选产物元数据（candidate_id / name）透传到结果中。
type PassthroughEvidenceValidator struct{}

func (PassthroughEvidenceValidator) Validate(_ context.Context, manifest model.ArtifactManifest, findings []model.Finding) (ValidatedEvidence, error) {
	return ValidatedEvidence{
		Artifacts: append([]model.Artifact(nil), manifest.Artifacts...),
		Findings:  append([]model.Finding(nil), findings...),
	}, nil
}

// RejectingEvidenceValidator 用于强安全审查模式。它安全地省略全部可选证据。
type RejectingEvidenceValidator struct{}

func (RejectingEvidenceValidator) Validate(context.Context, model.ArtifactManifest, []model.Finding) (ValidatedEvidence, error) {
	return ValidatedEvidence{}, nil
}

// ResourceProjector 在每次交付时重新鉴权并投影资源展示字段。
// 返回 allowed=false 表示资源当前不可交付；实现不得返回宿主绝对路径或敏感定位信息。
type ResourceProjector interface {
	ProjectArtifact(ctx context.Context, artifact model.Artifact) (projected model.Artifact, allowed bool, err error)
}

// DenyingResourceProjector 是无产品资源后端时的安全默认实现。
type DenyingResourceProjector struct{}

func (DenyingResourceProjector) ProjectArtifact(context.Context, model.Artifact) (model.Artifact, bool, error) {
	return model.Artifact{}, false, nil
}
