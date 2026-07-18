// Package pathresolver 提供本地工作区路径解析。
package pathresolver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// Resolver 将用户路径解析为本地 backend 路径，并标记路径范围。
type Resolver struct {
	workspaceRoot string
}

// New 创建本地路径解析器。
func New(workspaceRoot string) (*Resolver, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return nil, workcontract.NewError(workcontract.ErrCodeStateRootUnavailable, fmt.Errorf("path resolver 缺少显式 workspace root"))
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析 workspace root 失败: %w", err)
	}
	abs = filepath.Clean(abs)
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("解析 workspace real path 失败: %w", err)
	}
	return &Resolver{workspaceRoot: filepath.Clean(real)}, nil
}

// WorkspaceRoot 返回本地 workspace 根目录。
func (r *Resolver) WorkspaceRoot() string {
	return r.workspaceRoot
}

// Resolve 解析并校验路径类型。
func (r *Resolver) Resolve(ctx context.Context, ref model.PathRef, opts fscontract.ResolveOptions) (model.ResolvedPath, error) {
	select {
	case <-ctx.Done():
		return model.ResolvedPath{}, ctx.Err()
	default:
	}
	raw := strings.TrimSpace(ref.Raw)
	if raw == "" {
		return model.ResolvedPath{}, fscontract.NewError(fscontract.ErrCodeInvalidPath, "", fmt.Errorf("path不能为空"))
	}
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		return model.ResolvedPath{}, fscontract.NewError(fscontract.ErrCodeInvalidPath, raw, fmt.Errorf("路径解析缺少 PreparedRun；禁止回退到 bootstrap 静态根"))
	}
	candidate, _, err := workmodel.ExpandLogicalPath(raw, prepared.Execution.Workspace)
	if err != nil {
		return model.ResolvedPath{}, fscontract.NewError(fscontract.ErrCodeInvalidPath, raw, err)
	}
	if filepath.IsAbs(raw) && prepared.Execution.Binding.Mode != execmodel.WorkspaceModeProject && !withinExecutionWorkspace(candidate, prepared.Execution.Workspace) {
		return model.ResolvedPath{}, fscontract.NewError(fscontract.ErrCodePermissionDenied, raw, fmt.Errorf("%s 禁止访问 execution workspace 外的绝对路径", prepared.Execution.Binding.Mode))
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return model.ResolvedPath{}, fscontract.NewError(fscontract.ErrCodeInvalidPath, raw, err)
	}
	abs = filepath.Clean(abs)

	real, err := r.realPath(abs, opts.MustExist, opts.PreserveFinalSymlink)
	if err != nil {
		return model.ResolvedPath{}, err
	}
	scope, display, rel := r.scopeOfExecution(real, prepared.Execution.Workspace)
	if scope == model.PathScopeExternal && prepared.Execution.Binding.Mode != execmodel.WorkspaceModeProject {
		return model.ResolvedPath{}, fscontract.NewError(fscontract.ErrCodePermissionDenied, raw, fmt.Errorf("%s 路径越过 execution workspace", prepared.Execution.Binding.Mode))
	}
	if err := validateKind(real, raw, opts); err != nil {
		return model.ResolvedPath{}, err
	}

	if scope != model.PathScopeWorkspace {
		display = filepath.ToSlash(real)
	}
	return model.ResolvedPath{
		DisplayPath:  display,
		BackendPath:  real,
		WorkspaceRel: rel,
		WorkspaceID:  prepared.Execution.Binding.ID,
		Scope:        scope,
		RawPath:      raw,
	}, nil
}

func (r *Resolver) scopeOfExecution(real string, workspace execmodel.ExecutionWorkspace) (model.PathScope, string, string) {
	if isProtectedPath(real) {
		return model.PathScopeProtected, filepath.ToSlash(real), ""
	}
	type rootAlias struct{ root, prefix string }
	roots := []rootAlias{{workspace.WorkDir, ""}, {workspace.InputDir, "input"}, {workspace.OutputDir, "output"}, {workspace.TmpDir, "tmp"}, {workspace.SkillDir, "skills"}}
	for _, item := range roots {
		if strings.TrimSpace(item.root) == "" {
			continue
		}
		rootReal, err := resolveRootReal(item.root)
		if err != nil || !isWithin(real, rootReal) {
			continue
		}
		rel, err := filepath.Rel(rootReal, real)
		if err != nil {
			continue
		}
		display := filepath.ToSlash(rel)
		if display == "." {
			display = "."
		} else if item.prefix != "" {
			display = filepath.ToSlash(filepath.Join(item.prefix, rel))
		}
		return model.PathScopeWorkspace, display, display
	}
	return model.PathScopeExternal, filepath.ToSlash(real), ""
}

func resolveRootReal(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(real), nil
	}
	if os.IsNotExist(err) {
		return filepath.Clean(abs), nil
	}
	return "", err
}

func withinExecutionWorkspace(candidate string, workspace execmodel.ExecutionWorkspace) bool {
	for _, root := range []string{workspace.WorkDir, workspace.InputDir, workspace.OutputDir, workspace.TmpDir, workspace.SkillDir} {
		if strings.TrimSpace(root) != "" && isWithin(candidate, root) {
			return true
		}
	}
	return false
}

func validateKind(real string, raw string, opts fscontract.ResolveOptions) error {
	info, err := os.Lstat(real)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fscontract.NewError(fscontract.ErrCodeInvalidPath, raw, err)
	}
	if info.IsDir() && !opts.AllowDirectory {
		return fscontract.NewError(fscontract.ErrCodeInvalidInput, raw, fmt.Errorf("路径是目录，当前操作需要文件"))
	}
	if opts.RequireDirectory && !info.IsDir() {
		return fscontract.NewError(fscontract.ErrCodeNotDirectory, raw, fmt.Errorf("路径不是目录"))
	}
	return nil
}

func (r *Resolver) realPath(abs string, mustExist bool, preserveFinalSymlink bool) (string, error) {
	if preserveFinalSymlink && mustExist {
		parent := filepath.Dir(abs)
		realParent, err := filepath.EvalSymlinks(parent)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fscontract.NewError(fscontract.ErrCodeNotFound, abs, err)
			}
			return "", fscontract.NewError(fscontract.ErrCodeInvalidPath, abs, err)
		}
		return filepath.Clean(filepath.Join(realParent, filepath.Base(abs))), nil
	}
	if mustExist {
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fscontract.NewError(fscontract.ErrCodeNotFound, abs, err)
			}
			return "", fscontract.NewError(fscontract.ErrCodeInvalidPath, abs, err)
		}
		return filepath.Clean(real), nil
	}

	existing := abs
	missingParts := []string{}
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return filepath.Clean(abs), nil
		}
		missingParts = append([]string{filepath.Base(existing)}, missingParts...)
		existing = parent
	}
	realExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fscontract.NewError(fscontract.ErrCodeInvalidPath, abs, err)
	}
	parts := append([]string{realExisting}, missingParts...)
	return filepath.Clean(filepath.Join(parts...)), nil
}

func isWithin(candidate string, parent string) bool {
	c := normalize(candidate)
	p := normalize(parent)
	if c == p {
		return true
	}
	rel, err := filepath.Rel(p, c)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isProtectedPath(path string) bool {
	p := strings.ToLower(strings.ReplaceAll(filepath.Clean(path), "\\", "/"))
	protectedFragments := []string{
		"/windows/system32",
		"/windows/syswow64",
		"/program files",
		"/program files (x86)",
		"/etc/passwd",
		"/etc/shadow",
	}
	for _, fragment := range protectedFragments {
		if strings.Contains(p, fragment) {
			return true
		}
	}
	return p == ".ssh" || strings.HasSuffix(p, "/.ssh") || strings.Contains(p, "/.ssh/")
}

func normalize(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
