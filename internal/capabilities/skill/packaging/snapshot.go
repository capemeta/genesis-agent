// Package packaging 提供 Skill 包不可变摘要构建。
package packaging

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"genesis-agent/internal/capabilities/skill/model"
)

type File struct {
	Resource model.ResourceID
	Content  []byte
}

func BuildSnapshot(authority model.Authority, packageID model.PackageID, version string, files []File) (model.SkillPackageSnapshot, error) {
	if authority.Kind == "" || strings.TrimSpace(string(packageID)) == "" {
		return model.SkillPackageSnapshot{}, fmt.Errorf("skill package identity不完整")
	}
	if len(files) == 0 {
		return model.SkillPackageSnapshot{}, fmt.Errorf("skill package为空")
	}
	ordered := append([]File(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Resource < ordered[j].Resource })
	digests := make([]model.PackageFileDigest, 0, len(ordered))
	packageHash := sha256.New()
	seen := make(map[model.ResourceID]struct{}, len(ordered))
	manifestDigest := ""
	for _, file := range ordered {
		resource := model.ResourceID(strings.ReplaceAll(strings.TrimSpace(string(file.Resource)), `\`, "/"))
		if resource == "" || strings.Contains(string(resource), "..") || strings.ContainsRune(string(resource), '\x00') {
			return model.SkillPackageSnapshot{}, fmt.Errorf("skill package resource非法: %q", file.Resource)
		}
		if _, ok := seen[resource]; ok {
			return model.SkillPackageSnapshot{}, fmt.Errorf("skill package resource重复: %s", resource)
		}
		seen[resource] = struct{}{}
		sum := sha256.Sum256(file.Content)
		digest := hex.EncodeToString(sum[:])
		digests = append(digests, model.PackageFileDigest{Resource: resource, SHA256: digest, Size: int64(len(file.Content))})
		packageHash.Write([]byte(resource))
		packageHash.Write([]byte{0})
		packageHash.Write(sum[:])
		packageHash.Write([]byte{0})
		if strings.EqualFold(resourceBase(resource), model.RuntimeManifestFileName) {
			manifestDigest = digest
		}
	}
	return model.SkillPackageSnapshot{
		Authority: authority, PackageID: packageID, Version: strings.TrimSpace(version),
		Digest: hex.EncodeToString(packageHash.Sum(nil)), ManifestDigest: manifestDigest, Files: digests,
	}, nil
}

// ValidateSnapshot 重新计算完整包身份，并同时校验摘要和逐文件清单。只比较总摘要
// 不足以发现持久化元数据中的 size/hash 被单独篡改。
func ValidateSnapshot(expected model.SkillPackageSnapshot, files []File) error {
	actual, err := BuildSnapshot(expected.Authority, expected.PackageID, expected.Version, files)
	if err != nil {
		return err
	}
	if actual.Digest != expected.Digest || actual.ManifestDigest != expected.ManifestDigest || len(actual.Files) != len(expected.Files) {
		return fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: package snapshot摘要或清单不一致")
	}
	for i := range actual.Files {
		if actual.Files[i] != expected.Files[i] {
			return fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: package resource清单不一致: %s", actual.Files[i].Resource)
		}
	}
	return nil
}

func resourceBase(resource model.ResourceID) string {
	value := strings.TrimSuffix(string(resource), "/")
	if at := strings.LastIndex(value, "/"); at >= 0 {
		return value[at+1:]
	}
	return value
}
