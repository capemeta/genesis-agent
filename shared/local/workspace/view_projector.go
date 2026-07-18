package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// ViewProjector 把不可变快照同时投影到 InputDir（只读）和 WorkDir（工作副本）。
// 模型只看到 WorkDir 下的相对别名；InputDir 用于审计和显式只读访问。
type ViewProjector struct {
	snapshots workcontract.InputSnapshotReader
}

func NewViewProjector(snapshots workcontract.InputSnapshotReader) (*ViewProjector, error) {
	if snapshots == nil {
		return nil, fmt.Errorf("workspace view projector 缺少 input snapshot reader")
	}
	return &ViewProjector{snapshots: snapshots}, nil
}

func (p *ViewProjector) Project(ctx context.Context, execution workmodel.PreparedExecutionSnapshot, inputs workmodel.InputManifest) (workmodel.WorkspaceViewManifest, error) {
	if inputs.RunID != execution.Binding.Owner.RunID || inputs.BindingID != execution.Binding.ID {
		return workmodel.WorkspaceViewManifest{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("input manifest 与 execution binding 不匹配"))
	}
	view := workmodel.WorkspaceViewManifest{BindingID: execution.Binding.ID, Root: ".", Entries: make([]workmodel.WorkspaceViewEntry, 0, len(inputs.Inputs))}
	created := make([]string, 0, len(inputs.Inputs)*2)
	committed := false
	defer func() {
		if committed {
			return
		}
		for i := len(created) - 1; i >= 0; i-- {
			_ = os.Remove(created[i])
		}
	}()
	for _, input := range inputs.Inputs {
		if err := input.Alias.Validate(); err != nil {
			return workmodel.WorkspaceViewManifest{}, err
		}
		content, err := p.readVerified(ctx, input)
		if err != nil {
			return workmodel.WorkspaceViewManifest{}, err
		}
		inputTarget, err := safeProjectionTarget(execution.Workspace.InputDir, input.Alias)
		if err != nil {
			return workmodel.WorkspaceViewManifest{}, err
		}
		workTarget, err := safeProjectionTarget(execution.Workspace.WorkDir, input.Alias)
		if err != nil {
			return workmodel.WorkspaceViewManifest{}, err
		}
		if err := writeProjection(execution.Workspace.InputDir, inputTarget, content, 0o400); err != nil {
			return workmodel.WorkspaceViewManifest{}, err
		}
		created = append(created, inputTarget)
		if err := writeProjection(execution.Workspace.WorkDir, workTarget, content, 0o600); err != nil {
			return workmodel.WorkspaceViewManifest{}, err
		}
		created = append(created, workTarget)
		view.Entries = append(view.Entries, workmodel.WorkspaceViewEntry{Path: input.Alias, InputID: input.ID, Source: input.Source, Access: workmodel.WorkspaceViewAccessReadWrite})
	}
	committed = true
	return view, nil
}

func (p *ViewProjector) readVerified(ctx context.Context, input workmodel.InputRef) ([]byte, error) {
	reader, err := p.snapshots.OpenSnapshot(ctx, input.StagedPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, input.Size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) != input.Size {
		return nil, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("input snapshot %s 大小不一致", input.ID))
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != input.SHA256 {
		return nil, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("input snapshot %s hash 不一致", input.ID))
	}
	return content, nil
}

func safeProjectionTarget(root string, alias workmodel.WorkspacePath) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("workspace view 缺少投影根"))
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(absRoot, filepath.FromSlash(string(alias)))
	if !within(target, absRoot) {
		return "", workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("workspace view alias 越界: %s", alias))
	}
	return target, nil
}

func writeProjection(root, target string, content []byte, mode os.FileMode) error {
	if info, err := os.Lstat(target); err == nil {
		return workcontract.NewError(workcontract.ErrCodeInputNameConflict, fmt.Errorf("workspace view 禁止覆盖既有路径 %s（type=%s）", target, info.Mode().Type()))
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if err := rejectProjectionLinks(root, filepath.Dir(target)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".view-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}
	committed = true
	return nil
}

func rejectProjectionLinks(root, parent string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRoot, absParent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("workspace view parent 越界"))
	}
	current := absRoot
	parts := []string{}
	if rel != "." {
		parts = strings.Split(rel, string(filepath.Separator))
	}
	for _, part := range append([]string{""}, parts...) {
		if part != "" {
			current = filepath.Join(current, part)
		}
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return statErr
		}
		unsafe, unsafeErr := unsafeHostPathComponent(current)
		if unsafeErr != nil {
			return unsafeErr
		}
		if info.Mode()&os.ModeSymlink != 0 || unsafe {
			return workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("workspace view parent 含符号链接/reparse point: %s", current))
		}
		if !info.IsDir() {
			return workcontract.NewError(workcontract.ErrCodeInputNameConflict, fmt.Errorf("workspace view parent 不是目录: %s", current))
		}
	}
	return nil
}

var _ workcontract.WorkspaceViewProjector = (*ViewProjector)(nil)
