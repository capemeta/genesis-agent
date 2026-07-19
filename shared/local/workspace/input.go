package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	result, err := s.putAtomic(ctx, runID, inputID, name, content, -1, false)
	if err != nil {
		return "", err
	}
	return result.Path, nil
}

// PutCAS 按内容哈希寻址；同 Run 内相同 sha256+name 复用已有快照。
// maxBytes 必须 > 0；由调用方（InputStager）传入明确限额，禁止默认放大绕过 MaxTotal。
func (s *InputSnapshotStore) PutCAS(ctx context.Context, runID, name string, content io.Reader, maxBytes int64) (workcontract.PutCASResult, error) {
	if maxBytes <= 0 {
		return workcontract.PutCASResult{}, workcontract.NewError(workcontract.ErrCodeInputTooLarge, fmt.Errorf("PutCAS 缺少有效 maxBytes"))
	}
	return s.putAtomic(ctx, runID, "", name, content, maxBytes, true)
}

// LookupCAS 在已知内容哈希时查询快照，命中则可跳过再次读源写入。
func (s *InputSnapshotStore) LookupCAS(ctx context.Context, runID, sha256Hex, name string) (workcontract.PutCASResult, bool, error) {
	if err := ctx.Err(); err != nil {
		return workcontract.PutCASResult{}, false, err
	}
	digest := strings.ToLower(strings.TrimSpace(sha256Hex))
	if safeID(runID) != runID || filepath.Base(name) != name || strings.TrimSpace(name) == "" || !isSHA256Hex(digest) {
		return workcontract.PutCASResult{}, false, fmt.Errorf("非法 CAS 查询参数")
	}
	inputID := "cas-" + digest
	rel := filepath.ToSlash(filepath.Join("runtime", "runs", runID, "input", inputID, name))
	workspacePath := workmodel.WorkspacePath(rel)
	if err := workspacePath.Validate(); err != nil {
		return workcontract.PutCASResult{}, false, err
	}
	destination := filepath.Join(s.stateRoot, filepath.FromSlash(rel))
	info, err := os.Lstat(destination)
	if err != nil {
		if os.IsNotExist(err) {
			return workcontract.PutCASResult{}, false, nil
		}
		return workcontract.PutCASResult{}, false, err
	}
	if !info.Mode().IsRegular() {
		return workcontract.PutCASResult{}, false, fmt.Errorf("输入快照路径不是普通文件: %s", rel)
	}
	return workcontract.PutCASResult{
		Path: workspacePath, InputID: inputID, SHA256: digest, Size: info.Size(), Reused: true,
	}, true, nil
}

func isSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' {
			continue
		}
		return false
	}
	return true
}

func (s *InputSnapshotStore) putAtomic(ctx context.Context, runID, inputID, name string, content io.Reader, maxBytes int64, cas bool) (workcontract.PutCASResult, error) {
	if err := ctx.Err(); err != nil {
		return workcontract.PutCASResult{}, err
	}
	if safeID(runID) != runID || filepath.Base(name) != name || strings.TrimSpace(name) == "" {
		return workcontract.PutCASResult{}, fmt.Errorf("非法输入快照标识或名称")
	}
	if !cas && (safeID(inputID) != inputID || strings.TrimSpace(inputID) == "") {
		return workcontract.PutCASResult{}, fmt.Errorf("非法输入快照标识或名称")
	}

	dirRel := filepath.Join("runtime", "runs", runID, "input")
	baseDir := filepath.Join(s.stateRoot, dirRel)
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return workcontract.PutCASResult{}, err
	}
	tmp, err := os.CreateTemp(baseDir, ".input-*")
	if err != nil {
		return workcontract.PutCASResult{}, err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	hash := sha256.New()
	var written int64
	if maxBytes >= 0 {
		limited := &io.LimitedReader{R: content, N: maxBytes + 1}
		written, err = io.Copy(io.MultiWriter(tmp, hash), limited)
	} else {
		written, err = io.Copy(io.MultiWriter(tmp, hash), content)
	}
	if err != nil {
		return workcontract.PutCASResult{}, err
	}
	if maxBytes >= 0 && written > maxBytes {
		return workcontract.PutCASResult{}, workcontract.NewError(workcontract.ErrCodeInputTooLarge, fmt.Errorf("输入超过限额"))
	}
	if err := tmp.Sync(); err != nil {
		return workcontract.PutCASResult{}, err
	}
	if err := tmp.Close(); err != nil {
		return workcontract.PutCASResult{}, err
	}
	if err := os.Chmod(tmpPath, 0o400); err != nil {
		return workcontract.PutCASResult{}, err
	}

	digest := hex.EncodeToString(hash.Sum(nil))
	if cas {
		inputID = "cas-" + digest
	}
	rel := filepath.ToSlash(filepath.Join("runtime", "runs", runID, "input", inputID, name))
	workspacePath := workmodel.WorkspacePath(rel)
	if err := workspacePath.Validate(); err != nil {
		return workcontract.PutCASResult{}, err
	}
	destination := filepath.Join(s.stateRoot, filepath.FromSlash(rel))
	if info, err := os.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() {
			return workcontract.PutCASResult{}, fmt.Errorf("输入快照路径不是普通文件: %s", rel)
		}
		if cas {
			return workcontract.PutCASResult{Path: workspacePath, InputID: inputID, SHA256: digest, Size: info.Size(), Reused: true}, nil
		}
		return workcontract.PutCASResult{}, fmt.Errorf("输入快照已存在: %s", rel)
	} else if !os.IsNotExist(err) {
		return workcontract.PutCASResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return workcontract.PutCASResult{}, err
	}
	if err := os.Rename(tmpPath, destination); err != nil {
		return workcontract.PutCASResult{}, err
	}
	committed = true
	return workcontract.PutCASResult{Path: workspacePath, InputID: inputID, SHA256: digest, Size: written, Reused: false}, nil
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
