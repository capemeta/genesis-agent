package patch

import (
	"context"
	"fmt"
	"strings"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/model"
	"genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	"genesis-agent/internal/capabilities/tool/scheduler"
)

// Result 是 apply_patch 应用结果。
type Result struct {
	Added    []string `json:"added,omitempty"`
	Modified []string `json:"modified,omitempty"`
	Deleted  []string `json:"deleted,omitempty"`
	Summary  string   `json:"summary"`
}

// Service 应用 Codex 风格 patch。
type Service struct {
	deps toolkit.Deps
}

// NewService 创建 patch service。
func NewService(deps toolkit.Deps) (*Service, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}
	return &Service{deps: deps}, nil
}

// Apply 解析并应用 patch 文本。
func (s *Service) Apply(ctx context.Context, patchText string) (*Result, error) {
	hunks, err := Parse(patchText)
	if err != nil {
		return nil, err
	}
	resolved, locks, err := s.resolveAll(ctx, hunks)
	if err != nil {
		return nil, err
	}
	release, err := toolkit.Acquire(ctx, s.deps.Locker, locks)
	if err != nil {
		return nil, err
	}
	defer release()

	result := &Result{}
	for i, hunk := range hunks {
		paths := resolved[i]
		switch hunk.Type {
		case HunkAdd:
			if err := s.applyAdd(ctx, hunk, paths.source); err != nil {
				return nil, err
			}
			result.Added = append(result.Added, paths.source.DisplayPath)
		case HunkDelete:
			if err := s.applyDelete(ctx, paths.source); err != nil {
				return nil, err
			}
			result.Deleted = append(result.Deleted, paths.source.DisplayPath)
		case HunkUpdate:
			if err := s.applyUpdate(ctx, hunk, paths.source, paths.dest); err != nil {
				return nil, err
			}
			if paths.dest.BackendPath != "" {
				result.Modified = append(result.Modified, paths.dest.DisplayPath)
			} else {
				result.Modified = append(result.Modified, paths.source.DisplayPath)
			}
		}
	}
	result.Summary = result.summary()
	return result, nil
}

type resolvedHunk struct {
	source model.ResolvedPath
	dest   model.ResolvedPath
}

func (s *Service) resolveAll(ctx context.Context, hunks []Hunk) ([]resolvedHunk, []scheduler.ResourceLock, error) {
	resolved := make([]resolvedHunk, 0, len(hunks))
	locks := []scheduler.ResourceLock{}
	for _, hunk := range hunks {
		var item resolvedHunk
		var err error
		switch hunk.Type {
		case HunkAdd:
			item.source, err = s.resolveApproved(ctx, "apply_patch", hunk.Path, permission.OperationWrite, false)
		case HunkDelete:
			item.source, err = s.resolveDeleteApproved(ctx, hunk.Path)
		case HunkUpdate:
			if hunk.MovePath != "" {
				item.source, err = s.resolveMoveSourceApproved(ctx, hunk.Path)
			} else {
				item.source, err = s.resolveApproved(ctx, "apply_patch", hunk.Path, permission.OperationEdit, true)
			}
			if err == nil && hunk.MovePath != "" {
				item.dest, err = s.resolveApproved(ctx, "apply_patch", hunk.MovePath, permission.OperationWrite, false)
			}
		}
		if err != nil {
			return nil, nil, err
		}
		resolved = append(resolved, item)
		locks = append(locks, scheduler.ResourceLock{Scope: "workspace", Key: toolkit.WorkspaceLockKey(item.source), Mode: scheduler.LockWrite})
		locks = append(locks, scheduler.ResourceLock{Scope: "file", Key: toolkit.FileLockKey(item.source), Mode: scheduler.LockWrite})
		if item.dest.BackendPath != "" {
			locks = append(locks, scheduler.ResourceLock{Scope: "file", Key: toolkit.FileLockKey(item.dest), Mode: scheduler.LockWrite})
		}
	}
	return resolved, locks, nil
}

func (s *Service) resolveApproved(ctx context.Context, toolName string, raw string, op permission.Operation, mustExist bool) (model.ResolvedPath, error) {
	return toolkit.ResolveRequire(ctx, s.deps, toolName, raw, op, fscontract.ResolveOptions{Operation: string(op), MustExist: mustExist})
}

func (s *Service) resolveDeleteApproved(ctx context.Context, raw string) (model.ResolvedPath, error) {
	return toolkit.ResolveRequire(ctx, s.deps, "apply_patch", raw, permission.OperationDelete, fscontract.ResolveOptions{Operation: string(permission.OperationDelete), MustExist: true, PreserveFinalSymlink: true})
}

func (s *Service) resolveMoveSourceApproved(ctx context.Context, raw string) (model.ResolvedPath, error) {
	return toolkit.ResolveRequire(ctx, s.deps, "apply_patch", raw, permission.OperationEdit, fscontract.ResolveOptions{Operation: string(permission.OperationEdit), MustExist: true, PreserveFinalSymlink: true})
}

func (s *Service) applyAdd(ctx context.Context, hunk Hunk, path model.ResolvedPath) error {
	expectedHash, err := s.optionalHash(ctx, path)
	if err != nil {
		return err
	}
	content := []byte(hunk.Content)
	if err := s.deps.Backend.Write(ctx, path, content, fscontract.WriteOptions{CreateParents: true, Overwrite: true, Atomic: true, ExpectedHash: expectedHash}); err != nil {
		return err
	}
	return s.recordWrite(ctx, path, content)
}

func (s *Service) applyDelete(ctx context.Context, path model.ResolvedPath) error {
	stat, data, hash, err := s.current(ctx, path)
	if err != nil {
		return err
	}
	if stat.Type == model.EntryTypeDir {
		return fscontract.NewError(fscontract.ErrCodeInvalidInput, path.DisplayPath, fmt.Errorf("不能删除目录"))
	}
	if err := s.deps.Backend.Remove(ctx, path, fscontract.RemoveOptions{ExpectedHash: hash}); err != nil {
		return err
	}
	_ = data
	return nil
}

func (s *Service) applyUpdate(ctx context.Context, hunk Hunk, source model.ResolvedPath, dest model.ResolvedPath) error {
	stat, data, hash, err := s.current(ctx, source)
	if err != nil {
		return err
	}
	if stat.Type == model.EntryTypeDir {
		return fscontract.NewError(fscontract.ErrCodeInvalidInput, source.DisplayPath, fmt.Errorf("不能编辑目录"))
	}
	next, err := deriveNewContent(source.DisplayPath, string(data), hunk.Chunks)
	if err != nil {
		return err
	}
	if dest.BackendPath != "" {
		destHash, err := s.optionalHash(ctx, dest)
		if err != nil {
			return err
		}
		if err := s.deps.Backend.Write(ctx, dest, []byte(next), fscontract.WriteOptions{CreateParents: true, Overwrite: true, Atomic: true, ExpectedHash: destHash}); err != nil {
			return err
		}
		if err := s.deps.Backend.Remove(ctx, source, fscontract.RemoveOptions{ExpectedHash: hash}); err != nil {
			return err
		}
		return s.recordWrite(ctx, dest, []byte(next))
	}
	if err := s.deps.Backend.Write(ctx, source, []byte(next), fscontract.WriteOptions{Overwrite: true, Atomic: true, ExpectedHash: hash}); err != nil {
		return err
	}
	return s.recordWrite(ctx, source, []byte(next))
}

func (s *Service) current(ctx context.Context, path model.ResolvedPath) (*model.FileStat, []byte, string, error) {
	stat, err := s.deps.Backend.Stat(ctx, path)
	if err != nil {
		return nil, nil, "", err
	}
	data, err := s.deps.Backend.Read(ctx, path, fscontract.ReadOptions{MaxBytes: stat.Size})
	if err != nil {
		return nil, nil, "", err
	}
	return stat, data, toolkit.HashBytes(data), nil
}

func (s *Service) optionalHash(ctx context.Context, path model.ResolvedPath) (string, error) {
	_, data, hash, err := s.current(ctx, path)
	if err != nil {
		if fscontract.CodeOf(err) == fscontract.ErrCodeNotFound {
			return "", nil
		}
		return "", err
	}
	_ = data
	return hash, nil
}

func (s *Service) recordWrite(ctx context.Context, path model.ResolvedPath, data []byte) error {
	stat, err := s.deps.Backend.Stat(ctx, path)
	if err != nil {
		return err
	}
	return s.deps.Freshness.RecordWrite(ctx, path, *stat, toolkit.HashBytes(data))
}

func deriveNewContent(displayPath string, content string, chunks []Chunk) (string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	replacements, err := computeReplacements(displayPath, lines, chunks)
	if err != nil {
		return "", err
	}
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		next := make([]string, 0, len(lines)-r.oldLen+len(r.newLines))
		next = append(next, lines[:r.start]...)
		next = append(next, r.newLines...)
		next = append(next, lines[r.start+r.oldLen:]...)
		lines = next
	}
	return strings.Join(append(lines, ""), "\n"), nil
}

type replacement struct {
	start    int
	oldLen   int
	newLines []string
}

func computeReplacements(displayPath string, lines []string, chunks []Chunk) ([]replacement, error) {
	var out []replacement
	lineIndex := 0
	for _, chunk := range chunks {
		if chunk.ChangeContext != "" {
			idx := seekSequence(lines, []string{chunk.ChangeContext}, lineIndex, false)
			if idx < 0 {
				return nil, fmt.Errorf("Failed to find expected context in %s:\n%s", displayPath, chunk.ChangeContext)
			}
			lineIndex = idx + 1
		}
		pattern := chunk.OldLines
		newLines := chunk.NewLines
		found := seekSequence(lines, pattern, lineIndex, chunk.EndOfFile)
		if found < 0 && len(pattern) > 0 && pattern[len(pattern)-1] == "" {
			pattern = pattern[:len(pattern)-1]
			if len(newLines) > 0 && newLines[len(newLines)-1] == "" {
				newLines = newLines[:len(newLines)-1]
			}
			found = seekSequence(lines, pattern, lineIndex, chunk.EndOfFile)
		}
		if found < 0 {
			return nil, fmt.Errorf("Failed to find expected lines in %s:\n%s", displayPath, strings.Join(chunk.OldLines, "\n"))
		}
		out = append(out, replacement{start: found, oldLen: len(pattern), newLines: append([]string(nil), newLines...)})
		lineIndex = found + len(pattern)
	}
	return out, nil
}

func seekSequence(lines []string, pattern []string, start int, eof bool) int {
	if len(pattern) == 0 {
		if eof {
			return len(lines)
		}
		return start
	}
	if len(pattern) > len(lines) {
		return -1
	}
	searchStart := start
	if eof && len(lines) >= len(pattern) {
		searchStart = len(lines) - len(pattern)
	}
	matchers := []func(string, string) bool{
		func(a, b string) bool { return a == b },
		func(a, b string) bool { return strings.TrimRight(a, " \t") == strings.TrimRight(b, " \t") },
		func(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) },
		func(a, b string) bool { return normalizePunctuation(a) == normalizePunctuation(b) },
	}
	for _, matches := range matchers {
		for i := searchStart; i <= len(lines)-len(pattern); i++ {
			ok := true
			for j := range pattern {
				if !matches(lines[i+j], pattern[j]) {
					ok = false
					break
				}
			}
			if ok {
				return i
			}
		}
	}
	return -1
}

func normalizePunctuation(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch r {
		case '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2015', '\u2212':
			b.WriteRune('-')
		case '\u2018', '\u2019', '\u201A', '\u201B':
			b.WriteRune('\'')
		case '\u201C', '\u201D', '\u201E', '\u201F':
			b.WriteRune('"')
		case '\u00A0', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006', '\u2007', '\u2008', '\u2009', '\u200A', '\u202F', '\u205F', '\u3000':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (r *Result) summary() string {
	var b strings.Builder
	b.WriteString("Success. Updated the following files:\n")
	for _, p := range r.Added {
		b.WriteString("A ")
		b.WriteString(p)
		b.WriteByte('\n')
	}
	for _, p := range r.Modified {
		b.WriteString("M ")
		b.WriteString(p)
		b.WriteByte('\n')
	}
	for _, p := range r.Deleted {
		b.WriteString("D ")
		b.WriteString(p)
		b.WriteByte('\n')
	}
	return b.String()
}
