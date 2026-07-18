package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// ResourceRegistry 在产品控制面把已审批的本地路径冻结为版本化 ResourceRef。
// execution backend 只消费 ResourceRef，永远不重新解释模型提交的裸路径。
type ResourceRegistry struct {
	projectRoot string
	mu          sync.RWMutex
	resources   map[string]LocalResource
}

func NewResourceRegistry(projectRoot string) (*ResourceRegistry, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return nil, fmt.Errorf("resource registry 缺少 project root")
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	return &ResourceRegistry{projectRoot: filepath.Clean(root), resources: map[string]LocalResource{}}, nil
}

// ResolveInputs 将一次已批准工具调用中的路径转换为稳定资源引用。
func (r *ResourceRegistry) ResolveInputs(ctx context.Context, inputs []string) ([]workmodel.ResourceRef, error) {
	refs := make([]workmodel.ResourceRef, 0, len(inputs))
	for _, raw := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.HasPrefix(filepath.ToSlash(raw), "/workspace/") {
			return nil, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("执行面路径不能作为本地输入: %s", raw))
		}
		candidate := raw
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(r.projectRoot, filepath.FromSlash(candidate))
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			return nil, err
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, fmt.Errorf("解析输入 %q 失败: %w", raw, err)
		}
		info, err := os.Lstat(real)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, workcontract.NewError(workcontract.ErrCodeInputPermissionDenied, fmt.Errorf("输入不是普通文件: %s", raw))
		}
		if !filepath.IsAbs(raw) && !within(real, r.projectRoot) {
			return nil, workcontract.NewError(workcontract.ErrCodeInputPermissionDenied, fmt.Errorf("相对输入越过 project root: %s", raw))
		}
		version := localFileVersion(info)
		digest := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(real)) + "\x00" + version))
		id := "local-" + hex.EncodeToString(digest[:16])
		resource := LocalResource{Path: real, Version: version, MediaType: mime.TypeByExtension(filepath.Ext(real))}
		r.mu.Lock()
		r.resources[id] = resource
		r.mu.Unlock()
		scope := workmodel.ResourceScope{}
		refs = append(refs, workmodel.ResourceRef{Authority: "host", Scheme: "file", ID: id, Version: version, Scope: scope, Path: filepath.ToSlash(real)})
	}
	return refs, nil
}

// Open 按 ResourceRef 打开并复核文件身份版本，审批后变化会稳定失败。
func (r *ResourceRegistry) Open(ctx context.Context, ref workmodel.ResourceRef) (workcontract.ResourceHandle, error) {
	if err := ctx.Err(); err != nil {
		return workcontract.ResourceHandle{}, err
	}
	r.mu.RLock()
	resource, ok := r.resources[ref.ID]
	r.mu.RUnlock()
	if !ok || ref.Authority != "host" || ref.Scheme != "file" {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeInputPermissionDenied, fmt.Errorf("资源未注册: %s", ref.ID))
	}
	info, err := os.Lstat(resource.Path)
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("资源类型已变化: %s", ref.ID))
	}
	version := localFileVersion(info)
	if version != resource.Version || (ref.Version != "" && ref.Version != version) {
		return workcontract.ResourceHandle{}, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("资源版本已变化: %s", ref.ID))
	}
	file, err := os.Open(resource.Path)
	if err != nil {
		return workcontract.ResourceHandle{}, err
	}
	return workcontract.ResourceHandle{Reader: file, Size: info.Size(), Version: version, MediaType: resource.MediaType}, nil
}

func localFileVersion(info os.FileInfo) string {
	return fmt.Sprintf("stat:%d:%d", info.Size(), info.ModTime().UnixNano())
}

func within(candidate, root string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
