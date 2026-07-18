package workspace

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// LocalResource 是产品完成 PathResolver 和权限判断后注册的本地资源。
type LocalResource struct {
	Path      string
	Version   string
	SHA256    string
	Size      int64
	MediaType string
	Scope     workmodel.ResourceScope
	Identity  hostFileIdentity
}

// ResourceReader 只读取产品预先注册的资源 ID，不解析用户提交的裸路径。
type ResourceReader struct{ resources map[string]LocalResource }

// NewResourceReader 创建本地资源 reader，并复制注册表避免调用方并发修改。
func NewResourceReader(resources map[string]LocalResource) *ResourceReader {
	cloned := make(map[string]LocalResource, len(resources))
	for id, resource := range resources {
		cloned[id] = resource
	}
	return &ResourceReader{resources: cloned}
}

// Open 打开已授权的普通文件；符号链接必须在注册前由 PathResolver 解析。
func (r *ResourceReader) Open(ctx context.Context, ref workmodel.ResourceRef) (workcontract.ResourceHandle, error) {
	if err := ctx.Err(); err != nil {
		return workcontract.ResourceHandle{}, err
	}
	if ref.Authority != "host" {
		return workcontract.ResourceHandle{}, fmt.Errorf("本地 reader 不支持 authority %q", ref.Authority)
	}
	resource, ok := r.resources[ref.ID]
	if !ok {
		return workcontract.ResourceHandle{}, fmt.Errorf("资源 %s 未授权或未注册", ref.ID)
	}
	info, err := os.Lstat(resource.Path)
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return workcontract.ResourceHandle{}, fmt.Errorf("资源 %s 不是已解析的普通文件", ref.ID)
	}
	file, err := os.Open(resource.Path)
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	mediaType := resource.MediaType
	if mediaType == "" {
		mediaType = mime.TypeByExtension(filepath.Ext(resource.Path))
	}
	return workcontract.ResourceHandle{Reader: file, Size: info.Size(), Version: resource.Version, MediaType: mediaType}, nil
}

// InputSnapshotStore 将输入写到显式 state root 下的只读快照。
type InputSnapshotStore struct{ stateRoot string }

// NewInputSnapshotStore 创建本地输入快照 store。
func NewInputSnapshotStore(stateRoot string) (*InputSnapshotStore, error) {
	if strings.TrimSpace(stateRoot) == "" {
		return nil, workcontract.NewError(workcontract.ErrCodeStateRootUnavailable, fmt.Errorf("input store 缺少 state root"))
	}
	abs, err := filepath.Abs(stateRoot)
	if err != nil {
		return nil, workcontract.NewError(workcontract.ErrCodeStateRootUnavailable, err)
	}
	return &InputSnapshotStore{stateRoot: abs}, nil
}

// Put 原子写入输入快照，目标文件默认只允许当前用户读取。
func (s *InputSnapshotStore) Put(ctx context.Context, runID, inputID, name string, content io.Reader) (workmodel.WorkspacePath, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if safeID(runID) != runID || safeID(inputID) != inputID || filepath.Base(name) != name || strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("非法输入快照标识或名称")
	}
	rel := filepath.ToSlash(filepath.Join("runtime", "runs", runID, "input", inputID, name))
	workspacePath := workmodel.WorkspacePath(rel)
	if err := workspacePath.Validate(); err != nil {
		return "", err
	}
	destination := filepath.Join(s.stateRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".input-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, content); err != nil {
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmpPath, 0o400); err != nil {
		return "", err
	}
	if _, err := os.Lstat(destination); err == nil {
		return "", fmt.Errorf("输入快照已存在: %s", rel)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(tmpPath, destination); err != nil {
		return "", err
	}
	committed = true
	return workspacePath, nil
}

// Remove 清理由未完成 staging 产生的快照。
func (s *InputSnapshotStore) Remove(ctx context.Context, stagedPath workmodel.WorkspacePath) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := stagedPath.Validate(); err != nil {
		return err
	}
	target := filepath.Join(s.stateRoot, filepath.FromSlash(string(stagedPath)))
	if err := os.Chmod(target, 0o600); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// OpenSnapshot 打开 state root 内已经过校验的只读快照。
func (s *InputSnapshotStore) OpenSnapshot(ctx context.Context, stagedPath workmodel.WorkspacePath) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := stagedPath.Validate(); err != nil {
		return nil, err
	}
	target := filepath.Join(s.stateRoot, filepath.FromSlash(string(stagedPath)))
	info, err := os.Lstat(target)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("输入快照不是普通文件: %s", stagedPath)
	}
	return os.Open(target)
}
