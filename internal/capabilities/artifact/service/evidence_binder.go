package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// evidenceDeliverableSuffixes 可作为「用户可见交付」证据的后缀。
// 不含 .md：中间草稿/笔记频繁出现，不能仅因写出 markdown 就建 required 门禁。
var evidenceDeliverableSuffixes = map[string]struct{}{
	".pptx": {},
	".docx": {},
	".xlsx": {},
	".pdf":  {},
}

// EnsurePrimaryDeliverableFromProduced 在尚无 required primary Spec 时，按本 Run 已登记的可交付产物证据建约。
// FinalizeRequired 与 EvaluateCompletion 共用，保证「有产物证据 ⇒ 存在门禁契约」不变量。
func EnsurePrimaryDeliverableFromProduced(
	ctx context.Context,
	store artifactcontract.DeliverableSpecStore,
	tenantID, runID string,
	resources []workmodel.ProducedResourceDescriptor,
	now time.Time,
) error {
	if store == nil {
		return nil
	}
	specs, err := store.ListDeliverables(ctx, tenantID, runID)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if spec.Required && spec.Role == artifactmodel.DeliverableRolePrimary {
			return nil
		}
	}
	suffixes := collectEvidenceSuffixes(resources)
	if len(suffixes) == 0 {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	digest := sha256.Sum256([]byte(tenantID + "\x00" + runID + "\x00evidence-primary"))
	spec := artifactmodel.DeliverableSpec{
		ID: "deliverable-" + hex.EncodeToString(digest[:8]), TenantID: tenantID, RunID: runID,
		Required: true, Role: artifactmodel.DeliverableRolePrimary, DesiredName: "",
		// 仅约束后缀：避免 MIME 字符串变体（短名/长名）导致唯一产物无法匹配。
		AcceptedSuffix: suffixes, DeliveryPolicy: "run-output",
		CreatedAt: now,
	}
	if containsSuffix(suffixes, ".pptx") {
		spec.QAPolicy = "visual-qa/v1"
	}
	if hints, ok := artifactcontract.EvidenceQAHintsFromContext(ctx); ok {
		if hints.Policy != "" {
			spec.QAPolicy = hints.Policy
		}
		if hints.Enforcement != "" {
			spec.QAEnforcement = hints.Enforcement
		}
	}
	if err := spec.Validate(); err != nil {
		return err
	}
	if err := store.CreateDeliverable(ctx, spec); err != nil && err != artifactcontract.ErrAlreadyExists {
		return err
	}
	return nil
}

func (s *DeterministicFinalizer) ensurePrimaryFromProduced(ctx context.Context, tenantID, runID string, resources []workmodel.ProducedResourceDescriptor) error {
	return EnsurePrimaryDeliverableFromProduced(ctx, s.deliverables, tenantID, runID, resources, s.now())
}

func collectEvidenceSuffixes(resources []workmodel.ProducedResourceDescriptor) []string {
	seen := map[string]struct{}{}
	for _, resource := range resources {
		if skipEvidenceProduced(resource) {
			continue
		}
		ext := strings.ToLower(path.Ext(strings.TrimSpace(resource.ObservedName)))
		if _, ok := evidenceDeliverableSuffixes[ext]; !ok {
			continue
		}
		seen[ext] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for ext := range seen {
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}

func skipEvidenceProduced(resource workmodel.ProducedResourceDescriptor) bool {
	role := strings.ToLower(strings.TrimSpace(resource.Role))
	if role == "qa_asset" || role == "intermediate_asset" {
		return true
	}
	return isEvidenceQAPreviewName(resource.ObservedName)
}

func isEvidenceQAPreviewName(name string) bool {
	base := strings.ToLower(strings.TrimSpace(path.Base(name)))
	ext := path.Ext(base)
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
	default:
		return false
	}
	stem := strings.TrimSuffix(base, ext)
	if stem == "thumbnails" || stem == "thumbnail" {
		return true
	}
	return strings.HasPrefix(stem, "slide-") || strings.HasPrefix(stem, "thumbnail")
}

func containsSuffix(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
