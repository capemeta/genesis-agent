package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	"genesis-agent/internal/capabilities/skill/packaging"
)

const packageSnapshotMetadataFile = "snapshot.json"

// PackageStore 把完整 Skill 包按摘要持久化。目录提交是原子的，已存在摘要永不覆盖。
type PackageStore struct {
	dir string
	mu  sync.RWMutex
}

func NewPackageStore(stateRoot string) (*PackageStore, error) {
	stateRoot = strings.TrimSpace(stateRoot)
	if stateRoot == "" {
		return nil, fmt.Errorf("skill package state root 不能为空")
	}
	root, err := filepath.Abs(stateRoot)
	if err != nil || root == "" {
		return nil, fmt.Errorf("解析 skill package state root: %w", err)
	}
	dir := filepath.Join(root, "runtime", "skill-packages")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 skill package 目录: %w", err)
	}
	return &PackageStore{dir: dir}, nil
}

func (s *PackageStore) SavePackageSnapshot(ctx context.Context, snapshot skillmodel.SkillPackageSnapshot, files []skillmodel.SkillPackageFile) error {
	if err := validateStoredPackage(snapshot, files); err != nil {
		return err
	}
	if err := validateDigest(snapshot.Digest); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	finalDir := filepath.Join(s.dir, snapshot.Digest)
	if _, err := os.Stat(finalDir); err == nil {
		_, _, err = s.getUnlocked(snapshot.Digest)
		return err
	} else if !os.IsNotExist(err) {
		return err
	}
	tmpDir, err := os.MkdirTemp(s.dir, ".package-*")
	if err != nil {
		return fmt.Errorf("创建 skill package 临时目录: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(tmpDir)
		}
	}()
	contentsRoot := filepath.Join(tmpDir, "contents")
	for _, file := range files {
		rel, err := packageRelativePath(snapshot.PackageID, file.Resource)
		if err != nil {
			return err
		}
		target := filepath.Join(contentsRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, file.Content, 0o600); err != nil {
			return err
		}
	}
	meta, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, packageSnapshotMetadataFile), meta, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpDir, finalDir); err != nil {
		if _, statErr := os.Stat(finalDir); statErr == nil {
			_, _, verifyErr := s.getUnlocked(snapshot.Digest)
			return verifyErr
		}
		return fmt.Errorf("提交 skill package snapshot: %w", err)
	}
	committed = true
	return nil
}

func (s *PackageStore) GetPackageSnapshot(_ context.Context, digest string) (skillmodel.SkillPackageSnapshot, []skillmodel.SkillPackageFile, error) {
	if err := validateDigest(digest); err != nil {
		return skillmodel.SkillPackageSnapshot{}, nil, skillcontract.ErrSkillPackageSnapshotNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getUnlocked(strings.TrimSpace(digest))
}

func (s *PackageStore) getUnlocked(digest string) (skillmodel.SkillPackageSnapshot, []skillmodel.SkillPackageFile, error) {
	root := filepath.Join(s.dir, digest)
	data, err := os.ReadFile(filepath.Join(root, packageSnapshotMetadataFile))
	if errors.Is(err, os.ErrNotExist) {
		return skillmodel.SkillPackageSnapshot{}, nil, skillcontract.ErrSkillPackageSnapshotNotFound
	}
	if err != nil {
		return skillmodel.SkillPackageSnapshot{}, nil, err
	}
	var snapshot skillmodel.SkillPackageSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return skillmodel.SkillPackageSnapshot{}, nil, fmt.Errorf("解析 skill package snapshot: %w", err)
	}
	if snapshot.Digest != digest {
		return skillmodel.SkillPackageSnapshot{}, nil, fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: package目录与摘要不一致")
	}
	files := make([]skillmodel.SkillPackageFile, 0, len(snapshot.Files))
	for _, expected := range snapshot.Files {
		rel, err := packageRelativePath(snapshot.PackageID, expected.Resource)
		if err != nil {
			return skillmodel.SkillPackageSnapshot{}, nil, err
		}
		content, err := os.ReadFile(filepath.Join(root, "contents", filepath.FromSlash(rel)))
		if err != nil {
			return skillmodel.SkillPackageSnapshot{}, nil, fmt.Errorf("读取 skill package resource %s: %w", expected.Resource, err)
		}
		files = append(files, skillmodel.SkillPackageFile{Resource: expected.Resource, Content: content})
	}
	if err := validateStoredPackage(snapshot, files); err != nil {
		return skillmodel.SkillPackageSnapshot{}, nil, err
	}
	return cloneStoredSnapshot(snapshot), cloneStoredFiles(files), nil
}

func validateStoredPackage(snapshot skillmodel.SkillPackageSnapshot, files []skillmodel.SkillPackageFile) error {
	raw := make([]packaging.File, 0, len(files))
	for _, file := range files {
		raw = append(raw, packaging.File{Resource: file.Resource, Content: file.Content})
	}
	return packaging.ValidateSnapshot(snapshot, raw)
}

func packageRelativePath(packageID skillmodel.PackageID, resource skillmodel.ResourceID) (string, error) {
	prefix := string(packageID) + "/"
	rel := strings.TrimPrefix(strings.ReplaceAll(string(resource), `\`, "/"), prefix)
	if rel == "" || rel == string(resource) || strings.HasPrefix(rel, "/") || strings.Contains(rel, ":") {
		return "", fmt.Errorf("skill package resource非法: %s", resource)
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("skill package resource非法: %s", resource)
		}
	}
	return rel, nil
}

func validateDigest(value string) error {
	value = strings.TrimSpace(value)
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("skill package digest非法")
	}
	return nil
}

func cloneStoredSnapshot(in skillmodel.SkillPackageSnapshot) skillmodel.SkillPackageSnapshot {
	out := in
	out.Files = append([]skillmodel.PackageFileDigest(nil), in.Files...)
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Resource < out.Files[j].Resource })
	return out
}

func cloneStoredFiles(in []skillmodel.SkillPackageFile) []skillmodel.SkillPackageFile {
	out := make([]skillmodel.SkillPackageFile, len(in))
	for i, file := range in {
		out[i] = skillmodel.SkillPackageFile{Resource: file.Resource, Content: append([]byte(nil), file.Content...)}
	}
	return out
}
