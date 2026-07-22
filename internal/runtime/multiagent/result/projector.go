package result

import (
	"context"
	"path/filepath"
	"strings"

	"genesis-agent/internal/runtime/multiagent/model"
)

// Projector 将规范化结果投影给具体调用方。
type Projector struct{ resourceProjector ResourceProjector }

// NewProjector 创建结果投影器；nil 时安全地省略全部 artifact。
func NewProjector(resourceProjector ResourceProjector) Projector {
	if resourceProjector == nil {
		resourceProjector = DenyingResourceProjector{}
	}
	return Projector{resourceProjector: resourceProjector}
}

// Project 返回独立副本，避免调用方改写 Controller 中保存的终态记录。
func (p Projector) Project(ctx context.Context, record model.TaskResult) model.TaskResult {
	projected := record
	projected.Findings = cloneFindings(record.Findings)
	projected.Artifacts = nil
	for _, artifact := range record.Artifacts {
		resource, allowed, err := p.resourceProjector.ProjectArtifact(ctx, artifact)
		if err == nil && allowed && isSafeProjectedArtifact(resource) {
			projected.Artifacts = append(projected.Artifacts, resource)
			continue
		}
		projected.OmittedSections = append(projected.OmittedSections, "artifact")
	}
	return projected
}

func isSafeProjectedArtifact(artifact model.Artifact) bool {
	if strings.TrimSpace(artifact.ResourceID) == "" && strings.TrimSpace(artifact.CandidateID) == "" && strings.TrimSpace(artifact.Path) == "" {
		return false
	}
	path := strings.TrimSpace(artifact.Path)
	if path == "" {
		return true
	}
	path = strings.ReplaceAll(path, "\\", "/")
	if strings.HasPrefix(path, "/") || filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}
