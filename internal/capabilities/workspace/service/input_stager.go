// Package service 提供产品无关的工作空间编排服务。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"
	"time"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

const defaultMaxInputSize = int64(64 * 1024 * 1024)

// IDGenerator 生成稳定对象 ID。
type IDGenerator interface{ Generate() string }

// InputStager 实现受控输入 staging。
type InputStager struct {
	reader workcontract.ResourceReader
	store  workcontract.InputSnapshotStore
	ids    IDGenerator
	now    func() time.Time
}

// NewInputStager 创建输入 staging 服务。
func NewInputStager(reader workcontract.ResourceReader, store workcontract.InputSnapshotStore, ids IDGenerator) (*InputStager, error) {
	if reader == nil || store == nil || ids == nil {
		return nil, fmt.Errorf("input stager 缺少 reader/store/id generator")
	}
	return &InputStager{reader: reader, store: store, ids: ids, now: time.Now}, nil
}

// Stage 写入不可变快照，并返回 source 到 staged name 的显式映射。
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
	var stagedPaths []workmodel.WorkspacePath
	committed := false
	defer func() {
		if committed {
			return
		}
		for _, stagedPath := range stagedPaths {
			_ = s.store.Remove(context.Background(), stagedPath)
		}
	}()
	usedNames := map[string]int{}
	var total int64
	for _, source := range req.Sources {
		if err := validateResourceRef(source); err != nil {
			return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodeInputPermissionDenied, err)
		}
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
		name, err := stagedName(source, usedNames)
		if err != nil {
			_ = handle.Reader.Close()
			return workmodel.InputManifest{}, err
		}
		inputID := "input-" + s.ids.Generate()
		hash := sha256.New()
		limited := &io.LimitedReader{R: handle.Reader, N: maxFile + 1}
		stagedPath, putErr := s.store.Put(ctx, req.Binding.Owner.RunID, inputID, name, io.TeeReader(limited, hash))
		closeErr := handle.Reader.Close()
		if putErr != nil {
			return workmodel.InputManifest{}, fmt.Errorf("写入输入快照失败: %w", putErr)
		}
		stagedPaths = append(stagedPaths, stagedPath)
		if closeErr != nil {
			return workmodel.InputManifest{}, fmt.Errorf("关闭输入资源失败: %w", closeErr)
		}
		readSize := maxFile + 1 - limited.N
		if readSize > maxFile || total+readSize > maxTotal {
			_ = s.store.Remove(ctx, stagedPath)
			return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodeInputTooLarge, fmt.Errorf("输入 %s 超过限额", source.ID))
		}
		if err := stagedPath.Validate(); err != nil {
			return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, err)
		}
		digest := hex.EncodeToString(hash.Sum(nil))
		if strings.HasPrefix(source.Version, "sha256:") && strings.TrimPrefix(source.Version, "sha256:") != digest {
			_ = s.store.Remove(ctx, stagedPath)
			return workmodel.InputManifest{}, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("资源 %s hash 已变化", source.ID))
		}
		mediaType := handle.MediaType
		if mediaType == "" {
			mediaType = mime.TypeByExtension(path.Ext(name))
		}
		manifest.Inputs = append(manifest.Inputs, workmodel.InputRef{ID: inputID, Name: name, Size: readSize, SHA256: digest, MIME: mediaType, Source: source, StagedPath: stagedPath})
		total += readSize
	}
	committed = true
	return manifest, nil
}

func validateResourceRef(ref workmodel.ResourceRef) error {
	if strings.TrimSpace(ref.Authority) == "" || strings.TrimSpace(ref.Scheme) == "" || strings.TrimSpace(ref.ID) == "" {
		return fmt.Errorf("resource ref 缺少 authority/scheme/id")
	}
	return nil
}

func stagedName(ref workmodel.ResourceRef, used map[string]int) (string, error) {
	name := path.Base(strings.ReplaceAll(strings.TrimSpace(ref.Path), `\`, "/"))
	if name == "." || name == "/" || name == "" {
		name = strings.TrimSpace(ref.ID)
	}
	if name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return "", workcontract.NewError(workcontract.ErrCodeInputReservedPathConflict, fmt.Errorf("非法输入名称 %q", name))
	}
	key := strings.ToLower(name)
	used[key]++
	if used[key] == 1 {
		return name, nil
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s-%d%s", stem, used[key], ext), nil
}
