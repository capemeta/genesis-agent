// Package pathresolver 提供本地工作区路径解析。
package pathresolver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/model"
)

const defaultWorkspaceID = "local"

// Resolver 将用户路径解析为本地 backend 路径，并标记路径范围。
type Resolver struct {
	workspaceRoot string
	workspaceReal string
	workspaceID   string
}

// New 创建本地路径解析器。
func New(workspaceRoot string) (*Resolver, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("获取当前工作目录失败: %w", err)
		}
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
	return &Resolver{workspaceRoot: abs, workspaceReal: filepath.Clean(real), workspaceID: defaultWorkspaceID}, nil
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

	candidate := raw
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(r.workspaceRoot, candidate)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return model.ResolvedPath{}, fscontract.NewError(fscontract.ErrCodeInvalidPath, raw, err)
	}
	abs = filepath.Clean(abs)

	real, err := r.realPath(abs, opts.MustExist)
	if err != nil {
		return model.ResolvedPath{}, err
	}
	scope := r.scopeOf(real)
	if err := validateKind(real, raw, opts); err != nil {
		return model.ResolvedPath{}, err
	}

	rel := ""
	if scope == model.PathScopeWorkspace {
		workspaceRel, err := filepath.Rel(r.workspaceReal, real)
		if err != nil {
			return model.ResolvedPath{}, fscontract.NewError(fscontract.ErrCodeInvalidPath, raw, err)
		}
		if workspaceRel != "." {
			rel = filepath.ToSlash(workspaceRel)
		}
	}
	display := rel
	if scope != model.PathScopeWorkspace {
		display = filepath.ToSlash(real)
	}
	return model.ResolvedPath{
		DisplayPath:  display,
		BackendPath:  real,
		WorkspaceRel: rel,
		WorkspaceID:  r.workspaceID,
		Scope:        scope,
		RawPath:      raw,
	}, nil
}

func (r *Resolver) scopeOf(real string) model.PathScope {
	if isProtectedPath(real) {
		return model.PathScopeProtected
	}
	if isWithin(real, r.workspaceReal) {
		return model.PathScopeWorkspace
	}
	return model.PathScopeExternal
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

func (r *Resolver) realPath(abs string, mustExist bool) (string, error) {
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
