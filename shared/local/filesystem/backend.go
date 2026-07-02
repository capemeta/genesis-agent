// Package fs_backend 提供本地文件系统 backend。
package fs_backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/model"
)

const (
	defaultReadMaxBytes = int64(512 * 1024)
	maxReadBytes        = int64(512 * 1024 * 1024)
	defaultListMax      = 1000
	maxListEntries      = 10000
	defaultWalkDepth    = 8
	defaultWalkDirs     = 1000
	defaultWalkEntries  = 10000
	defaultWalkBytes    = int64(4 * 1024 * 1024)
	maxWalkDepth        = 64
	maxWalkDirs         = 10000
	maxWalkEntries      = 50000
	maxWalkBytes        = int64(4 * 1024 * 1024)
)

// Backend 是本地宿主机文件系统实现。
type Backend struct{}

// New 创建本地文件系统 backend。
func New() *Backend {
	return &Backend{}
}

func (b *Backend) Read(ctx context.Context, path model.ResolvedPath, opts fscontract.ReadOptions) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultReadMaxBytes
	}
	if maxBytes > maxReadBytes {
		return nil, fscontract.NewError(fscontract.ErrCodeInvalidInput, path.DisplayPath, fmt.Errorf("max_bytes超过上限%d", maxReadBytes))
	}
	file, err := os.Open(path.BackendPath)
	if err != nil {
		return nil, mapOSError(path.DisplayPath, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fscontract.NewError(fscontract.ErrCodeInvalidInput, path.DisplayPath, err)
	}
	if int64(len(data)) > maxBytes {
		return data[:maxBytes], fscontract.NewError(fscontract.ErrCodeTooLarge, path.DisplayPath, fmt.Errorf("文件超过最大读取字节数%d", maxBytes))
	}
	return data, nil
}

func (b *Backend) Write(ctx context.Context, path model.ResolvedPath, content []byte, opts fscontract.WriteOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if opts.CreateParents {
		if err := os.MkdirAll(filepath.Dir(path.BackendPath), 0o755); err != nil {
			return mapOSError(path.DisplayPath, err)
		}
	}
	if !opts.Overwrite {
		if _, err := os.Stat(path.BackendPath); err == nil {
			return fscontract.NewError(fscontract.ErrCodeAlreadyExists, path.DisplayPath, os.ErrExist)
		} else if err != nil && !os.IsNotExist(err) {
			return mapOSError(path.DisplayPath, err)
		}
	}
	if opts.ExpectedHash != "" {
		currentHash, err := hashFile(path.BackendPath)
		if err != nil {
			return mapOSError(path.DisplayPath, err)
		}
		if currentHash != opts.ExpectedHash {
			return fscontract.NewError(fscontract.ErrCodeModifiedExternally, path.DisplayPath, fmt.Errorf("expected_hash不匹配"))
		}
	}
	if opts.Atomic {
		return atomicWrite(path, content)
	}
	if err := os.WriteFile(path.BackendPath, content, 0o644); err != nil {
		return mapOSError(path.DisplayPath, err)
	}
	return nil
}

func (b *Backend) ListDir(ctx context.Context, path model.ResolvedPath, opts fscontract.ListOptions) ([]model.DirEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = defaultListMax
	}
	if maxEntries > maxListEntries {
		return nil, fscontract.NewError(fscontract.ErrCodeInvalidInput, path.DisplayPath, fmt.Errorf("max_entries超过上限%d", maxListEntries))
	}
	entries, err := os.ReadDir(path.BackendPath)
	if err != nil {
		return nil, mapOSError(path.DisplayPath, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	if len(entries) > maxEntries {
		entries = entries[:maxEntries]
	}
	out := make([]model.DirEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, mapOSError(entry.Name(), err)
		}
		out = append(out, dirEntry(path, entry.Name(), info))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type == model.EntryTypeDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (b *Backend) Walk(ctx context.Context, path model.ResolvedPath, opts fscontract.WalkOptions) (*model.WalkOutcome, error) {
	if opts.FollowSymlinks {
		return nil, fscontract.NewError(fscontract.ErrCodeInvalidInput, path.DisplayPath, fmt.Errorf("follow_symlinks第一轮暂不支持"))
	}
	limits := fillWalkLimits(opts)
	if err := validateWalkLimits(path, limits); err != nil {
		return nil, err
	}
	out := &model.WalkOutcome{Root: path.DisplayPath}
	rootDepth := pathDepth(path.BackendPath)
	errStop := fmt.Errorf("walk stopped")
	err := filepath.WalkDir(path.BackendPath, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			if current == path.BackendPath {
				return mapOSError(current, err)
			}
			out.Errors = append(out.Errors, model.WalkError{Path: displayChildPath(path, current), Message: err.Error()})
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if current == path.BackendPath {
			return nil
		}
		depth := pathDepth(current) - rootDepth
		if depth > limits.MaxDepth {
			out.Truncated = true
			out.LimitCause = "max_depth"
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			out.Errors = append(out.Errors, model.WalkError{Path: displayChildPath(path, current), Message: err.Error()})
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		childPath := path
		childPath.DisplayPath = displayChildPath(path, current)
		out.Entries = append(out.Entries, dirEntry(childPath, "", info))
		if info.IsDir() {
			out.DirsSeen++
		} else {
			out.FilesSeen++
			out.BytesSeen += info.Size()
		}
		if len(out.Entries) >= limits.MaxEntries {
			out.Truncated = true
			out.LimitCause = "max_entries"
			return errStop
		}
		if out.DirsSeen >= limits.MaxDirs {
			out.Truncated = true
			out.LimitCause = "max_dirs"
			return errStop
		}
		if out.BytesSeen >= limits.MaxBytes {
			out.Truncated = true
			out.LimitCause = "max_bytes"
			return errStop
		}
		return nil
	})
	if err != nil && err != errStop {
		return nil, err
	}
	return out, nil
}

func (b *Backend) Stat(ctx context.Context, path model.ResolvedPath) (*model.FileStat, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path.BackendPath)
	if err != nil {
		return nil, mapOSError(path.DisplayPath, err)
	}
	stat := &model.FileStat{
		Path:        path,
		Type:        entryType(info),
		Size:        info.Size(),
		ModifiedAt:  info.ModTime(),
		IsSymlink:   info.Mode()&os.ModeSymlink != 0,
		Permissions: info.Mode().Perm().String(),
	}
	if stat.IsSymlink {
		if target, err := os.Readlink(path.BackendPath); err == nil {
			stat.TargetPath = target
		}
	}
	return stat, nil
}

func (b *Backend) MkdirAll(ctx context.Context, path model.ResolvedPath, _ fscontract.MkdirOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(path.BackendPath, 0o755); err != nil {
		return mapOSError(path.DisplayPath, err)
	}
	return nil
}

// HashBytes 返回 sha256 hex。
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func atomicWrite(path model.ResolvedPath, content []byte) error {
	dir := filepath.Dir(path.BackendPath)
	tmp, err := os.CreateTemp(dir, ".genesis-*")
	if err != nil {
		return mapOSError(path.DisplayPath, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return mapOSError(path.DisplayPath, err)
	}
	if err := tmp.Close(); err != nil {
		return mapOSError(path.DisplayPath, err)
	}
	if err := replaceFile(tmpName, path.BackendPath); err != nil {
		return mapOSError(path.DisplayPath, err)
	}
	cleanup = false
	return nil
}

func mapOSError(path string, err error) error {
	switch {
	case os.IsNotExist(err):
		return fscontract.NewError(fscontract.ErrCodeNotFound, path, err)
	case os.IsExist(err):
		return fscontract.NewError(fscontract.ErrCodeAlreadyExists, path, err)
	case os.IsPermission(err):
		return fscontract.NewError(fscontract.ErrCodePermissionDenied, path, err)
	default:
		return err
	}
}

func dirEntry(parent model.ResolvedPath, name string, info os.FileInfo) model.DirEntry {
	display := parent.DisplayPath
	if name != "" {
		display = filepath.ToSlash(filepath.Join(display, name))
	}
	return model.DirEntry{
		Name:       chooseName(name, info.Name()),
		Path:       display,
		Type:       entryType(info),
		Size:       info.Size(),
		ModifiedAt: info.ModTime(),
	}
}

func chooseName(name string, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

func entryType(info os.FileInfo) model.EntryType {
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return model.EntryTypeSymlink
	case info.IsDir():
		return model.EntryTypeDir
	case mode.IsRegular():
		return model.EntryTypeFile
	default:
		return model.EntryTypeOther
	}
}

func fillWalkLimits(opts fscontract.WalkOptions) fscontract.WalkOptions {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultWalkDepth
	}
	if opts.MaxDirs <= 0 {
		opts.MaxDirs = defaultWalkDirs
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = defaultWalkEntries
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = defaultWalkBytes
	}
	return opts
}

func validateWalkLimits(path model.ResolvedPath, opts fscontract.WalkOptions) error {
	if opts.MaxDepth > maxWalkDepth {
		return fscontract.NewError(fscontract.ErrCodeInvalidInput, path.DisplayPath, fmt.Errorf("max_depth超过上限%d", maxWalkDepth))
	}
	if opts.MaxDirs > maxWalkDirs {
		return fscontract.NewError(fscontract.ErrCodeInvalidInput, path.DisplayPath, fmt.Errorf("max_dirs超过上限%d", maxWalkDirs))
	}
	if opts.MaxEntries > maxWalkEntries {
		return fscontract.NewError(fscontract.ErrCodeInvalidInput, path.DisplayPath, fmt.Errorf("max_entries超过上限%d", maxWalkEntries))
	}
	if opts.MaxBytes > maxWalkBytes {
		return fscontract.NewError(fscontract.ErrCodeInvalidInput, path.DisplayPath, fmt.Errorf("max_bytes超过上限%d", maxWalkBytes))
	}
	return nil
}

func displayChildPath(root model.ResolvedPath, current string) string {
	rel, err := filepath.Rel(root.BackendPath, current)
	if err != nil || rel == "." {
		return root.DisplayPath
	}
	return filepath.ToSlash(filepath.Join(root.DisplayPath, rel))
}

func pathDepth(path string) int {
	if path == "" {
		return 0
	}
	vol := filepath.VolumeName(path)
	trimmed := filepath.Clean(path[len(vol):])
	trimmed = strings.Trim(trimmed, string(filepath.Separator))
	if trimmed == "" || trimmed == "." {
		return 0
	}
	return len(strings.Split(trimmed, string(filepath.Separator)))
}
