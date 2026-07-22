package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	"genesis-agent/internal/capabilities/skill/packaging"
)

type packageRecord struct {
	snapshot model.SkillPackageSnapshot
	files    []model.SkillPackageFile
}

// PackageStore 是测试和短生命周期嵌入场景使用的内容寻址包存储。
type PackageStore struct {
	mu     sync.RWMutex
	byHash map[string]packageRecord
}

func NewPackageStore() *PackageStore {
	return &PackageStore{byHash: make(map[string]packageRecord)}
}

func (s *PackageStore) SavePackageSnapshot(_ context.Context, snapshot model.SkillPackageSnapshot, files []model.SkillPackageFile) error {
	if err := validatePackageSnapshot(snapshot, files); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.byHash[snapshot.Digest]; ok {
		if prior.snapshot.PackageID != snapshot.PackageID || prior.snapshot.Authority != snapshot.Authority {
			return fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: package digest identity冲突")
		}
		return nil
	}
	s.byHash[snapshot.Digest] = packageRecord{snapshot: cloneSnapshot(snapshot), files: clonePackageFiles(files)}
	return nil
}

func (s *PackageStore) GetPackageSnapshot(_ context.Context, digest string) (model.SkillPackageSnapshot, []model.SkillPackageFile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.byHash[strings.TrimSpace(digest)]
	if !ok {
		return model.SkillPackageSnapshot{}, nil, contract.ErrSkillPackageSnapshotNotFound
	}
	return cloneSnapshot(record.snapshot), clonePackageFiles(record.files), nil
}

func validatePackageSnapshot(snapshot model.SkillPackageSnapshot, files []model.SkillPackageFile) error {
	if strings.TrimSpace(snapshot.Digest) == "" {
		return fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: package digest为空")
	}
	raw := make([]packaging.File, 0, len(files))
	for _, file := range files {
		raw = append(raw, packaging.File{Resource: file.Resource, Content: file.Content})
	}
	return packaging.ValidateSnapshot(snapshot, raw)
}

func cloneSnapshot(in model.SkillPackageSnapshot) model.SkillPackageSnapshot {
	out := in
	out.Files = append([]model.PackageFileDigest(nil), in.Files...)
	return out
}

func clonePackageFiles(in []model.SkillPackageFile) []model.SkillPackageFile {
	out := make([]model.SkillPackageFile, len(in))
	for i, file := range in {
		out[i] = model.SkillPackageFile{Resource: file.Resource, Content: append([]byte(nil), file.Content...)}
	}
	return out
}
