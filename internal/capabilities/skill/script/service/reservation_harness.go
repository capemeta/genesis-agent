package service

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxsession "genesis-agent/internal/capabilities/sandbox/session"
	"genesis-agent/internal/platform/idgen"
)

// prepareOutputReservations 在执行前为 required Deliverable 分配逻辑写入槽，并展开为受控 env。
// 物理根由 OUTPUT_DIR 决定；reservation service 只返回逻辑相对路径。
func (s *Service) prepareOutputReservations(ctx context.Context, binding execmodel.ExecutionBinding, runID, outputDir string) (artifactcontract.ReserveResult, map[string]string, error) {
	if s.reservations == nil {
		return artifactcontract.ReserveResult{}, nil, nil
	}
	attemptID := "attempt-" + idgen.NewUUIDGenerator().Generate()
	result, err := s.reservations.Reserve(ctx, artifactcontract.ReserveRequest{
		TenantID: binding.Owner.TenantID, RunID: runID, BindingID: binding.ID, AttemptID: attemptID,
	})
	if err != nil {
		return artifactcontract.ReserveResult{}, nil, err
	}
	env := map[string]string{}
	for key, logical := range result.EnvBindings {
		env[key] = joinReservationPath(outputDir, logical)
	}
	return result, env, nil
}

func ensureLocalReservationDirs(outputDir string, reservations []artifactmodel.OutputReservation) error {
	if isRemoteNamespacePath(outputDir) {
		return nil
	}
	for _, reservation := range reservations {
		physical := joinReservationPath(outputDir, string(reservation.LogicalTarget))
		if err := os.MkdirAll(filepath.Dir(physical), 0o755); err != nil {
			return fmt.Errorf("创建 reservation 目录失败: %w", err)
		}
	}
	return nil
}

func ensureRemoteReservationDirs(ctx context.Context, session *sandboxsession.Session, outputDir string, reservations []artifactmodel.OutputReservation) error {
	for _, reservation := range reservations {
		physical := joinReservationPath(outputDir, string(reservation.LogicalTarget))
		dir := path.Dir(normalizeSlash(physical))
		if err := session.MkdirAll(ctx, sandboxsession.RelativePath(dir, ""), fscontract.MkdirOptions{}); err != nil {
			return fmt.Errorf("创建远程 reservation 目录失败: %w", err)
		}
	}
	return nil
}

func mergeReservedEnv(base, reserved map[string]string) map[string]string {
	if len(reserved) == 0 {
		return base
	}
	out := make(map[string]string, len(base)+len(reserved))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range reserved {
		out[k] = v
	}
	return out
}

func joinReservationPath(outputDir, logical string) string {
	logical = strings.TrimSpace(strings.ReplaceAll(logical, `\`, "/"))
	if isRemoteNamespacePath(outputDir) {
		return path.Join(strings.TrimRight(outputDir, "/"), logical)
	}
	return filepath.Join(outputDir, filepath.FromSlash(logical))
}

func isRemoteNamespacePath(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "/")
}

func collectReservedHitsLocal(outputDir, skillDir string, reservations []artifactmodel.OutputReservation, observed map[string]fileFingerprint) []string {
	hits := make([]string, 0, len(reservations))
	for _, reservation := range reservations {
		logical := string(reservation.LogicalTarget)
		physical := joinReservationPath(outputDir, logical)
		if rel, ok := relativeUnderRoot(skillDir, physical); ok {
			if _, exists := observed[rel]; exists {
				hits = append(hits, rel)
			}
			continue
		}
		// task_job：OUTPUT_DIR 与 skillDir 分离时，直接探测 reservation 物理文件。
		info, err := os.Stat(physical)
		if err != nil || info == nil || info.IsDir() {
			continue
		}
		if safeRemoteRelativePath(logical) {
			hits = append(hits, logical)
			observed[logical] = fileFingerprint{Size: info.Size(), ModTime: info.ModTime().UnixNano()}
		}
	}
	return hits
}

func collectReservedHitsRemote(ctx context.Context, session *sandboxsession.Session, outputDir, skillDir string, reservations []artifactmodel.OutputReservation, observed map[string]fileFingerprint) []string {
	hits := make([]string, 0, len(reservations))
	for _, reservation := range reservations {
		physical := joinReservationPath(outputDir, string(reservation.LogicalTarget))
		if rel, ok := relativeUnderRoot(skillDir, physical); ok {
			if _, exists := observed[rel]; exists {
				hits = append(hits, rel)
			}
			continue
		}
		stat, err := session.Stat(ctx, sandboxsession.RelativePath(physical, ""))
		if err != nil || stat == nil || stat.Type != fsmodel.EntryTypeFile {
			continue
		}
		candidate := normalizeSlash(strings.TrimPrefix(strings.TrimPrefix(physical, "/workspace/"), "/"))
		if safeRemoteRelativePath(candidate) {
			hits = append(hits, candidate)
			observed[candidate] = fileFingerprint{Size: stat.Size, ModTime: stat.ModifiedAt.UnixNano()}
		}
	}
	return hits
}

func relativeUnderRoot(root, target string) (string, bool) {
	if isRemoteNamespacePath(root) {
		root = strings.TrimRight(normalizeSlash(root), "/")
		target = normalizeSlash(target)
		prefix := root + "/"
		if strings.HasPrefix(target, prefix) {
			rel := strings.TrimPrefix(target, prefix)
			return rel, safeRemoteRelativePath(rel)
		}
		return "", false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, safeRemoteRelativePath(rel)
}

func mergeProducedCandidates(reserved, discovered []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(reserved)+len(discovered))
	for _, item := range append(append([]string{}, reserved...), discovered...) {
		item = normalizeSlash(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

// filterProducedByDeliverables 在存在 DeliverableSpec 时过滤 diff 候选：
// reservation 命中始终保留；其余候选须匹配任一交付契约的后缀/MIME。
// 无 DeliverableSpec 时保持原候选集（非交付任务不误伤诊断产出）。
func (s *Service) filterProducedByDeliverables(ctx context.Context, tenantID, runID string, reservedHits, candidates []string) ([]string, error) {
	if s.deliverables == nil || len(candidates) == 0 {
		return candidates, nil
	}
	specs, err := s.deliverables.ListDeliverables(ctx, tenantID, runID)
	if err != nil {
		return nil, err
	}
	if len(specs) == 0 {
		return candidates, nil
	}
	keepReserved := map[string]struct{}{}
	for _, item := range reservedHits {
		keepReserved[normalizeSlash(item)] = struct{}{}
	}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = normalizeSlash(candidate)
		if _, ok := keepReserved[candidate]; ok {
			out = append(out, candidate)
			continue
		}
		name := path.Base(candidate)
		media := mime.TypeByExtension(path.Ext(candidate))
		for _, spec := range specs {
			if spec.MatchesObserved(name, media) {
				out = append(out, candidate)
				break
			}
		}
	}
	return out, nil
}
