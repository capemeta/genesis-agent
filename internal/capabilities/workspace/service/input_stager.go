// Package service 提供产品无关的工作空间编排服务。
package service

import (
	"context"
	"fmt"
	"mime"
	"path"
	"strings"
	"time"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

const defaultMaxInputSize = int64(64 * 1024 * 1024)

// IDGenerator 生成稳定对象 ID（保留供装配兼容；CAS 路径使用内容哈希，不再依赖此生成器）。
type IDGenerator interface{ Generate() string }

// InputStager 实现受控输入 staging。
type InputStager struct {
	reader workcontract.ResourceReader
	store  workcontract.InputSnapshotStore
	now    func() time.Time
}

// NewInputStager 创建输入 staging 服务。ids 可为 nil（历史参数，CAS 不再使用）。
func NewInputStager(reader workcontract.ResourceReader, store workcontract.InputSnapshotStore, ids IDGenerator) (*InputStager, error) {
	if reader == nil || store == nil {
		return nil, fmt.Errorf("input stager 缺少 reader/store")
	}
	_ = ids
	return &InputStager{reader: reader, store: store, now: time.Now}, nil
}

// Stage 写入不可变快照，并返回 source 到 staged name 的显式映射。
// 同 Run 内相同内容与文件名通过 PutCAS 复用物理快照；每次 Stage 仍产生独立 Manifest 条目。
// 当 ResourceRef/Handle 已带可信 sha256 Version 且 LookupCAS 命中时，仍 Open 做权限与变更校验，
// 但跳过 PutCAS 二次读流与写 temp（无静默错复用副作用）。
func (s *InputStager) Stage(ctx context.Context, req workcontract.StageRequest) (workmodel.InputManifest, error) {
	if err := req.Binding.Validate(); err != nil {
		return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, err)
	}
	maxFile := req.MaxFileSize
	if maxFile <= 0 {
		maxFile = defaultMaxInputSize
	}
	maxTotal := req.MaxTotal
	if maxTotal <= 0 {
		maxTotal = maxFile * 4
	}
	manifest := workmodel.InputManifest{RunID: req.Binding.Owner.RunID, BindingID: req.Binding.ID, CreatedAt: s.now().UTC()}
	var createdPaths []workmodel.WorkspacePath
	committed := false
	defer func() {
		if committed {
			return
		}
		for _, stagedPath := range createdPaths {
			_ = s.store.Remove(context.Background(), stagedPath)
		}
	}()
	usedAliases := map[string]int{}
	var total int64
	for _, source := range req.Sources {
		if err := validateResourceRef(source); err != nil {
			return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodeInputPermissionDenied, err)
		}
		owner := req.Binding.Owner
		if source.Scope.TenantID != owner.TenantID || source.Scope.ProjectID != owner.ProjectID || source.Scope.UserID != owner.UserID {
			return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodeInputPermissionDenied, fmt.Errorf("资源 %s scope 与 execution binding 不一致", source.ID))
		}
		alias, err := stagedAlias(source, usedAliases)
		if err != nil {
			return workmodel.InputManifest{}, err
		}
		name := path.Base(string(alias))
		remainingTotal := maxTotal - total
		limit := maxFile
		if remainingTotal < limit {
			limit = remainingTotal
		}
		if limit <= 0 {
			return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodeInputTooLarge, fmt.Errorf("输入 %s 超过限额", source.ID))
		}

		// 先 Open：保留权限与宿主内容变更检测；再决定是否跳过 PutCAS。
		handle, err := s.reader.Open(ctx, source)
		if err != nil {
			return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodeInputPermissionDenied, err)
		}
		if handle.Reader == nil {
			return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodeInputPermissionDenied, fmt.Errorf("资源 reader 为空"))
		}
		if source.Version != "" && handle.Version != "" && source.Version != handle.Version {
			_ = handle.Reader.Close()
			return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("资源 %s 版本已变化", source.ID))
		}
		if handle.Size > maxFile || (handle.Size >= 0 && total+handle.Size > maxTotal) {
			_ = handle.Reader.Close()
			return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodeInputTooLarge, fmt.Errorf("输入 %s 超过限额", source.ID))
		}

		digest := trustedContentSHA256(source.Version, handle.Version)
		if digest != "" {
			if hit, ok, lookupErr := s.store.LookupCAS(ctx, req.Binding.Owner.RunID, digest, name); lookupErr != nil {
				_ = handle.Reader.Close()
				return workmodel.InputManifest{}, fmt.Errorf("查询输入快照失败: %w", lookupErr)
			} else if ok {
				_ = handle.Reader.Close()
				if hit.Size > maxFile || total+hit.Size > maxTotal {
					return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodeInputTooLarge, fmt.Errorf("输入 %s 超过限额", source.ID))
				}
				mediaType := handle.MediaType
				if mediaType == "" {
					mediaType = mime.TypeByExtension(path.Ext(name))
				}
				manifest.Inputs = append(manifest.Inputs, workmodel.InputRef{
					ID: hit.InputID, Name: name, Alias: alias, Size: hit.Size,
					SHA256: hit.SHA256, MIME: mediaType, Source: source, StagedPath: hit.Path,
				})
				total += hit.Size
				continue
			}
		}

		casResult, putErr := s.store.PutCAS(ctx, req.Binding.Owner.RunID, name, handle.Reader, limit)
		closeErr := handle.Reader.Close()
		if putErr != nil {
			return workmodel.InputManifest{}, fmt.Errorf("写入输入快照失败: %w", putErr)
		}
		if closeErr != nil {
			if !casResult.Reused {
				_ = s.store.Remove(ctx, casResult.Path)
			}
			return workmodel.InputManifest{}, fmt.Errorf("关闭输入资源失败: %w", closeErr)
		}
		if !casResult.Reused {
			createdPaths = append(createdPaths, casResult.Path)
		}
		if err := casResult.Path.Validate(); err != nil {
			return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, err)
		}
		if digest != "" && digest != casResult.SHA256 {
			if !casResult.Reused {
				_ = s.store.Remove(ctx, casResult.Path)
			}
			return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("资源 %s hash 已变化", source.ID))
		}
		mediaType := handle.MediaType
		if mediaType == "" {
			mediaType = mime.TypeByExtension(path.Ext(name))
		}
		manifest.Inputs = append(manifest.Inputs, workmodel.InputRef{
			ID: casResult.InputID, Name: name, Alias: alias, Size: casResult.Size,
			SHA256: casResult.SHA256, MIME: mediaType, Source: source, StagedPath: casResult.Path,
		})
		total += casResult.Size
	}
	committed = true
	return manifest, nil
}

func trustedContentSHA256(values ...string) string {
	for _, raw := range values {
		raw = strings.TrimSpace(strings.ToLower(raw))
		if !strings.HasPrefix(raw, "sha256:") {
			continue
		}
		digest := strings.TrimPrefix(raw, "sha256:")
		if len(digest) == 64 {
			ok := true
			for _, r := range digest {
				if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' {
					continue
				}
				ok = false
				break
			}
			if ok {
				return digest
			}
		}
	}
	return ""
}

func validateResourceRef(ref workmodel.ResourceRef) error {
	if strings.TrimSpace(ref.Authority) == "" || strings.TrimSpace(ref.Scheme) == "" || strings.TrimSpace(ref.ID) == "" {
		return fmt.Errorf("resource ref 缺少 authority/scheme/id")
	}
	return nil
}

func stagedAlias(ref workmodel.ResourceRef, used map[string]int) (workmodel.WorkspacePath, error) {
	raw := strings.TrimSpace(strings.ReplaceAll(ref.Path, `\`, "/"))
	alias := workmodel.WorkspacePath(raw)
	if err := alias.Validate(); err != nil {
		fallback := path.Base(raw)
		if fallback == "." || fallback == "/" || fallback == "" {
			fallback = strings.TrimSpace(ref.ID)
		}
		alias = workmodel.WorkspacePath(fallback)
		if fallbackErr := alias.Validate(); fallbackErr != nil {
			return "", workcontract.NewError(workcontract.ErrCodeInputReservedPathConflict, fmt.Errorf("非法输入别名 %q: %w", raw, fallbackErr))
		}
	}
	key := strings.ToLower(string(alias))
	used[key]++
	if used[key] == 1 {
		return alias, nil
	}
	dir, name := path.Split(string(alias))
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	return workmodel.WorkspacePath(path.Join(dir, fmt.Sprintf("%s-%d%s", stem, used[key], ext))), nil
}
