package subagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"genesis-agent/internal/runtime/multiagent/model"
	"genesis-agent/internal/runtime/multiagent/result"
)

var safeResourceID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// WorkspaceResources 验证并投影 CLI 工作区中的显式登记产物。
type WorkspaceResources struct {
	root string
}

// NewWorkspaceResources 创建受工作区边界约束的资源后端。
func NewWorkspaceResources(workspaceRoot string) (*WorkspaceResources, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析工作区路径失败: %w", err)
	}
	return &WorkspaceResources{root: filepath.Clean(abs)}, nil
}

func (r *WorkspaceResources) Validate(ctx context.Context, manifest model.ArtifactManifest, findings []model.Finding) (result.ValidatedEvidence, error) {
	if err := ctx.Err(); err != nil {
		return result.ValidatedEvidence{}, err
	}
	artifacts := make([]model.Artifact, 0, len(manifest.Artifacts))
	allowed := map[string]struct{}{}
	for _, candidate := range manifest.Artifacts {
		artifact, ok, err := r.validateArtifact(candidate)
		if err != nil {
			return result.ValidatedEvidence{}, err
		}
		if !ok {
			continue
		}
		artifacts = append(artifacts, artifact)
		if artifact.ResourceID != "" {
			allowed[artifact.ResourceID] = struct{}{}
		}
		if artifact.Path != "" {
			allowed[artifact.Path] = struct{}{}
		}
	}
	return result.ValidatedEvidence{
		Artifacts: artifacts,
		Findings:  filterFindings(findings, allowed),
	}, nil
}

func (r *WorkspaceResources) ProjectArtifact(ctx context.Context, artifact model.Artifact) (model.Artifact, bool, error) {
	if err := ctx.Err(); err != nil {
		return model.Artifact{}, false, err
	}
	projected, ok, err := r.validateArtifact(artifact)
	if err != nil || !ok {
		return model.Artifact{}, ok, err
	}
	if strings.TrimSpace(artifact.ContentHash) != "" && artifact.ContentHash != projected.ContentHash {
		return model.Artifact{}, false, nil
	}
	return projected, true, nil
}

func (r *WorkspaceResources) validateArtifact(candidate model.Artifact) (model.Artifact, bool, error) {
	rel := cleanRelative(candidate.Path)
	if rel == "" {
		return model.Artifact{}, false, nil
	}
	abs := filepath.Join(r.root, filepath.FromSlash(rel))
	abs = filepath.Clean(abs)
	if !inside(r.root, abs) {
		return model.Artifact{}, false, nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return model.Artifact{}, false, nil
		}
		return model.Artifact{}, false, fmt.Errorf("读取 subagent artifact 信息失败: %w", err)
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return model.Artifact{}, false, nil
	}
	hash, err := fileSHA256(abs)
	if err != nil {
		return model.Artifact{}, false, err
	}
	resourceID := strings.TrimSpace(candidate.ResourceID)
	if !safeResourceID.MatchString(resourceID) {
		resourceID = "res-" + hash[:24]
	}
	return model.Artifact{
		ResourceID:  resourceID,
		Path:        rel,
		Kind:        firstNonEmpty(candidate.Kind, "file"),
		Description: strings.TrimSpace(candidate.Description),
		ContentHash: hash,
	}, true, nil
}

func cleanRelative(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" || strings.HasPrefix(raw, "/") || filepath.IsAbs(raw) || filepath.VolumeName(raw) != "" {
		return ""
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(raw)))
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return ""
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == ".." || segment == "" {
			return ""
		}
	}
	return clean
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开 subagent artifact 失败: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("计算 subagent artifact hash 失败: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func filterFindings(findings []model.Finding, allowed map[string]struct{}) []model.Finding {
	if len(findings) == 0 || len(allowed) == 0 {
		return nil
	}
	out := make([]model.Finding, 0, len(findings))
	for _, finding := range findings {
		claim := strings.TrimSpace(finding.Claim)
		if claim == "" || len(finding.Evidence) == 0 {
			continue
		}
		evidence := make([]string, 0, len(finding.Evidence))
		for _, ref := range finding.Evidence {
			ref = strings.TrimSpace(strings.ReplaceAll(ref, "\\", "/"))
			if _, ok := allowed[ref]; ok {
				evidence = append(evidence, ref)
			}
		}
		if len(evidence) > 0 {
			out = append(out, model.Finding{Claim: claim, Evidence: evidence})
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
