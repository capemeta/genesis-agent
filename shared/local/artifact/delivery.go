package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// TargetRegistry 保存产品已解析和授权的本地目录，业务层只传播 ResourceRef.ID。
type TargetRegistry struct{ directories map[string]string }

func NewTargetRegistry(directories map[string]string) (*TargetRegistry, error) {
	cloned := make(map[string]string, len(directories))
	for id, directory := range directories {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(directory) == "" {
			return nil, fmt.Errorf("交付目标 id/path 不能为空")
		}
		abs, err := filepath.Abs(directory)
		if err != nil {
			return nil, err
		}
		cloned[id] = abs
	}
	return &TargetRegistry{directories: cloned}, nil
}

// GetMaterialized verifies an existing target by content hash; existence alone never counts as success.
func (m *Materializer) GetMaterialized(ctx context.Context, artifact artifactmodel.ArtifactRef, target artifactmodel.DeliveryTarget) (artifactmodel.DeliveryResult, bool, error) {
	if err := ctx.Err(); err != nil {
		return artifactmodel.DeliveryResult{}, false, err
	}
	if target.Kind == artifactmodel.DeliveryArtifactOnly {
		return artifactmodel.DeliveryResult{Artifact: artifact, Target: target, Resource: artifact.StorageRef}, true, nil
	}
	dir, ok := m.targets.directories[target.Resource.ID]
	if !ok || target.Resource.Authority != "host" || !safeComponent(target.Name) {
		return artifactmodel.DeliveryResult{}, false, deliveryFailure(artifact, artifactcontract.ErrCodeDeliveryTargetDenied, fmt.Errorf("目标未授权"))
	}
	destination := filepath.Join(dir, target.Name)
	info, err := os.Lstat(destination)
	if os.IsNotExist(err) {
		return artifactmodel.DeliveryResult{}, false, nil
	}
	if err != nil {
		return artifactmodel.DeliveryResult{}, false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return artifactmodel.DeliveryResult{}, false, nil
	}
	file, err := os.Open(destination)
	if err != nil {
		return artifactmodel.DeliveryResult{}, false, err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return artifactmodel.DeliveryResult{}, false, copyErr
	}
	if closeErr != nil {
		return artifactmodel.DeliveryResult{}, false, closeErr
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), artifact.SHA256) {
		return artifactmodel.DeliveryResult{}, false, nil
	}
	resource := workmodel.ResourceRef{Authority: "host", Scheme: "delivery", ID: target.Resource.ID, Path: filepath.ToSlash(target.Name), Version: "sha256:" + artifact.SHA256, MediaType: artifact.MIME, Scope: artifact.Scope}
	return artifactmodel.DeliveryResult{Artifact: artifact, Target: target, Resource: resource, Display: destination}, true, nil
}

func (r *TargetRegistry) CanWrite(ctx context.Context, target workmodel.ResourceRef, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, ok := r.directories[target.ID]
	if !ok || target.Authority != "host" {
		return fmt.Errorf("目标未授权")
	}
	if !safeComponent(name) {
		return fmt.Errorf("非法交付文件名")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("目标目录不可用")
	}
	probe, err := os.CreateTemp(dir, ".delivery-probe-*")
	if err != nil {
		return fmt.Errorf("目标目录不可写: %w", err)
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)
	return nil
}

// Materializer 原子导出且默认拒绝覆盖同名文件；同 deliverable supersede 走 ReplaceMaterialize。
type Materializer struct {
	store   artifactcontract.Store
	targets *TargetRegistry
}

func NewMaterializer(store artifactcontract.Store, targets *TargetRegistry) (*Materializer, error) {
	if store == nil || targets == nil {
		return nil, fmt.Errorf("delivery materializer 缺少 store/targets")
	}
	return &Materializer{store: store, targets: targets}, nil
}

func (m *Materializer) Materialize(ctx context.Context, artifact artifactmodel.ArtifactRef, target artifactmodel.DeliveryTarget) (artifactmodel.DeliveryResult, error) {
	return m.materialize(ctx, artifact, target, false)
}

func (m *Materializer) ReplaceMaterialize(ctx context.Context, artifact artifactmodel.ArtifactRef, target artifactmodel.DeliveryTarget) (artifactmodel.DeliveryResult, error) {
	return m.materialize(ctx, artifact, target, true)
}

func (m *Materializer) materialize(ctx context.Context, artifact artifactmodel.ArtifactRef, target artifactmodel.DeliveryTarget, replace bool) (artifactmodel.DeliveryResult, error) {
	if target.Kind == artifactmodel.DeliveryArtifactOnly {
		return artifactmodel.DeliveryResult{Artifact: artifact, Target: target, Resource: artifact.StorageRef}, nil
	}
	if err := m.targets.CanWrite(ctx, target.Resource, target.Name); err != nil {
		return artifactmodel.DeliveryResult{}, deliveryFailure(artifact, artifactcontract.ErrCodeDeliveryTargetDenied, err)
	}
	dir := m.targets.directories[target.Resource.ID]
	destination := filepath.Join(dir, target.Name)
	info, err := os.Lstat(destination)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return artifactmodel.DeliveryResult{}, deliveryFailure(artifact, artifactcontract.ErrCodeDeliveryMaterializeFailed, err)
	}
	if exists {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return artifactmodel.DeliveryResult{}, deliveryFailure(artifact, artifactcontract.ErrCodeDeliveryTargetConflict, fmt.Errorf("目标不是普通文件: %s", target.Name))
		}
		if !replace {
			return artifactmodel.DeliveryResult{}, deliveryFailure(artifact, artifactcontract.ErrCodeDeliveryTargetConflict, fmt.Errorf("目标已存在: %s", target.Name))
		}
	}
	reader, err := m.store.Open(ctx, artifact)
	if err != nil {
		return artifactmodel.DeliveryResult{}, deliveryFailure(artifact, artifactcontract.ErrCodeDeliveryMaterializeFailed, err)
	}
	defer reader.Close()
	tmp, err := os.CreateTemp(dir, ".delivery-*")
	if err != nil {
		return artifactmodel.DeliveryResult{}, deliveryFailure(artifact, artifactcontract.ErrCodeDeliveryMaterializeFailed, err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, reader); err != nil {
		return artifactmodel.DeliveryResult{}, deliveryFailure(artifact, artifactcontract.ErrCodeDeliveryMaterializeFailed, err)
	}
	if err := tmp.Sync(); err != nil {
		return artifactmodel.DeliveryResult{}, deliveryFailure(artifact, artifactcontract.ErrCodeDeliveryMaterializeFailed, err)
	}
	if err := tmp.Close(); err != nil {
		return artifactmodel.DeliveryResult{}, deliveryFailure(artifact, artifactcontract.ErrCodeDeliveryMaterializeFailed, err)
	}
	if replace && exists {
		if err := replaceLedgerFile(tmpPath, destination); err != nil {
			return artifactmodel.DeliveryResult{}, deliveryFailure(artifact, artifactcontract.ErrCodeDeliveryMaterializeFailed, err)
		}
	} else {
		if err := os.Rename(tmpPath, destination); err != nil {
			return artifactmodel.DeliveryResult{}, deliveryFailure(artifact, artifactcontract.ErrCodeDeliveryMaterializeFailed, err)
		}
	}
	committed = true
	resource := workmodel.ResourceRef{Authority: "host", Scheme: "delivery", ID: target.Resource.ID, Path: filepath.ToSlash(target.Name), Version: "sha256:" + artifact.SHA256, MediaType: artifact.MIME, Scope: artifact.Scope}
	return artifactmodel.DeliveryResult{Artifact: artifact, Target: target, Resource: resource, Display: destination}, nil
}

func deliveryFailure(artifact artifactmodel.ArtifactRef, code artifactcontract.ErrorCode, err error) error {
	copy := artifact
	return &artifactcontract.Error{Code: code, Err: err, Artifact: &copy}
}

var _ artifactcontract.RecoverableMaterializer = (*Materializer)(nil)
