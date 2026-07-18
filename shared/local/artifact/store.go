// Package artifact 提供 CLI/Desktop 共享的本地 ArtifactStore 与交付适配。
package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// Store 使用 state root 下独立 artifacts 目录，不依赖 Run 生命周期。
type Store struct{ root string }

func NewStore(stateRoot string) (*Store, error) {
	if strings.TrimSpace(stateRoot) == "" {
		return nil, fmt.Errorf("artifact store 缺少 state root")
	}
	abs, err := filepath.Abs(filepath.Join(stateRoot, "artifacts", "blobs"))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(abs, ".quarantine"), 0o700); err != nil {
		return nil, err
	}
	return &Store{root: abs}, nil
}

func (s *Store) Stage(ctx context.Context, artifactID, name string, content io.Reader) (artifactcontract.StagedObject, error) {
	if err := ctx.Err(); err != nil {
		return artifactcontract.StagedObject{}, err
	}
	if !safeComponent(artifactID) || !safeComponent(name) {
		return artifactcontract.StagedObject{}, artifactcontract.NewError(artifactcontract.ErrCodeArtifactPathInvalid, fmt.Errorf("非法 artifact id/name"))
	}
	file, err := os.CreateTemp(filepath.Join(s.root, ".quarantine"), ".artifact-*")
	if err != nil {
		return artifactcontract.StagedObject{}, err
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := io.Copy(file, content); err != nil {
		return artifactcontract.StagedObject{}, err
	}
	if err := file.Sync(); err != nil {
		return artifactcontract.StagedObject{}, err
	}
	if err := file.Close(); err != nil {
		return artifactcontract.StagedObject{}, err
	}
	ok = true
	return artifactcontract.StagedObject{ID: artifactID, Name: filepath.Base(path)}, nil
}

func (s *Store) OpenStaged(ctx context.Context, object artifactcontract.StagedObject) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.Open(s.stagedPath(object))
}

func (s *Store) Commit(ctx context.Context, object artifactcontract.StagedObject, manifest artifactmodel.Manifest) (artifactmodel.ArtifactRef, error) {
	if err := ctx.Err(); err != nil {
		return artifactmodel.ArtifactRef{}, err
	}
	if manifest.ID != object.ID || !safeComponent(manifest.ID) || !safeComponent(manifest.Name) {
		return artifactmodel.ArtifactRef{}, artifactcontract.NewError(artifactcontract.ErrCodeArtifactPathInvalid, fmt.Errorf("manifest 与 quarantine 不一致"))
	}
	dir := filepath.Join(s.root, manifest.ID)
	if _, err := os.Lstat(dir); err == nil {
		return artifactmodel.ArtifactRef{}, artifactcontract.NewError(artifactcontract.ErrCodeArtifactNameConflict, fmt.Errorf("Artifact %s 已存在", manifest.ID))
	} else if !os.IsNotExist(err) {
		return artifactmodel.ArtifactRef{}, err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return artifactmodel.ArtifactRef{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(dir)
		}
	}()
	destination := filepath.Join(dir, manifest.Name)
	if err := os.Rename(s.stagedPath(object), destination); err != nil {
		return artifactmodel.ArtifactRef{}, err
	}
	manifest.StorageRef = workmodel.ResourceRef{Authority: "host", Scheme: "artifact", ID: manifest.ID, Path: filepath.ToSlash(filepath.Join("artifacts", manifest.ID, manifest.Name)), Version: "sha256:" + manifest.SHA256, MediaType: manifest.MIME, Scope: manifest.Scope}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return artifactmodel.ArtifactRef{}, err
	}
	if err := writeExclusive(filepath.Join(dir, "manifest.json"), data); err != nil {
		return artifactmodel.ArtifactRef{}, err
	}
	committed = true
	return manifest.ArtifactRef, nil
}

func (s *Store) Abort(_ context.Context, object artifactcontract.StagedObject) error {
	err := os.Remove(s.stagedPath(object))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Store) Open(ctx context.Context, artifact artifactmodel.ArtifactRef) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !safeComponent(artifact.ID) || !safeComponent(artifact.Name) {
		return nil, artifactcontract.NewError(artifactcontract.ErrCodeArtifactPathInvalid, fmt.Errorf("非法 ArtifactRef"))
	}
	return os.Open(filepath.Join(s.root, artifact.ID, artifact.Name))
}

// GetCommitted 按确定性 ID 读取已提交 manifest，用于 commit 成功但 ledger 更新前崩溃后的恢复。
func (s *Store) GetCommitted(ctx context.Context, artifactID string) (artifactmodel.ArtifactRef, bool, error) {
	if err := ctx.Err(); err != nil {
		return artifactmodel.ArtifactRef{}, false, err
	}
	if !safeComponent(artifactID) {
		return artifactmodel.ArtifactRef{}, false, artifactcontract.NewError(artifactcontract.ErrCodeArtifactPathInvalid, fmt.Errorf("非法 Artifact ID"))
	}
	data, err := os.ReadFile(filepath.Join(s.root, artifactID, "manifest.json"))
	if os.IsNotExist(err) {
		return artifactmodel.ArtifactRef{}, false, nil
	}
	if err != nil {
		return artifactmodel.ArtifactRef{}, false, err
	}
	var manifest artifactmodel.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return artifactmodel.ArtifactRef{}, false, fmt.Errorf("读取 Artifact manifest: %w", err)
	}
	if manifest.ID != artifactID || manifest.StorageRef.Version != "sha256:"+manifest.SHA256 {
		return artifactmodel.ArtifactRef{}, false, artifactcontract.NewError(artifactcontract.ErrCodeArtifactInvalid, fmt.Errorf("Artifact manifest 身份或版本无效"))
	}
	return manifest.ArtifactRef, true, nil
}

func (s *Store) stagedPath(object artifactcontract.StagedObject) string {
	return filepath.Join(s.root, ".quarantine", filepath.Base(object.Name))
}

func safeComponent(value string) bool {
	return strings.TrimSpace(value) != "" && filepath.Base(value) == value && value != "." && value != ".." && !strings.ContainsRune(value, '\x00')
}

func writeExclusive(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
