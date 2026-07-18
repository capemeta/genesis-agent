package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// GitChangeGuard 使用 Run 开始时的完整 Git 工作区状态作为基线。
// 它比较的是 RunStartBaseline -> 当前状态，不要求用户原有工作树干净。
type GitChangeGuard struct {
	mu        sync.Mutex
	baselines map[string]string
}

func NewGitChangeGuard() *GitChangeGuard {
	return &GitChangeGuard{baselines: make(map[string]string)}
}

func (g *GitChangeGuard) InitializeRun(ctx context.Context, prepared workmodel.PreparedRun) error {
	if !prepared.Manifest.ProjectChangeRequired {
		return nil
	}
	if prepared.Execution.Binding.Mode != execmodel.WorkspaceModeProject {
		return fmt.Errorf("声明项目变更的 Run 未绑定 project_workspace")
	}
	root := strings.TrimSpace(prepared.Manifest.ProjectDir)
	if root == "" {
		return fmt.Errorf("项目变更门禁缺少已授权项目根")
	}
	snapshot, err := gitWorkspaceSnapshot(ctx, root)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.baselines[prepared.Manifest.RunID] = snapshot
	g.mu.Unlock()
	return nil
}

func (g *GitChangeGuard) EvaluateCompletion(ctx context.Context, prepared workmodel.PreparedRun) (workcontract.CompletionDecision, error) {
	if !prepared.Manifest.ProjectChangeRequired {
		return workcontract.CompletionDecision{Complete: true}, nil
	}
	g.mu.Lock()
	baseline, ok := g.baselines[prepared.Manifest.RunID]
	g.mu.Unlock()
	if !ok {
		return workcontract.CompletionDecision{}, fmt.Errorf("项目变更门禁缺少 RunStartBaseline")
	}
	current, err := gitWorkspaceSnapshot(ctx, prepared.Manifest.ProjectDir)
	if err != nil {
		return workcontract.CompletionDecision{}, err
	}
	if current == baseline {
		return workcontract.CompletionDecision{Complete: false, Reminder: "项目开发任务尚未产生相对于 Run 开始基线的文件增量。请完成实际修改并运行必要验证；不要用用户原有 dirty 状态冒充本 Run 变更。"}, nil
	}
	return workcontract.CompletionDecision{Complete: true}, nil
}

// ReleaseRun 清理内存基线；Run 的所有退出路径都由 app 层 defer 调用。
func (g *GitChangeGuard) ReleaseRun(prepared workmodel.PreparedRun) {
	g.mu.Lock()
	delete(g.baselines, prepared.Manifest.RunID)
	g.mu.Unlock()
}

func gitWorkspaceSnapshot(ctx context.Context, root string) (string, error) {
	absRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil || strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("项目根无效: %w", err)
	}
	if err := runGitDiscard(ctx, absRoot, "rev-parse", "--is-inside-work-tree"); err != nil {
		return "", fmt.Errorf("项目变更门禁要求 Git 工作树: %w", err)
	}
	hash := sha256.New()
	commands := [][]string{
		{"status", "--porcelain=v1", "-z", "--untracked-files=all"},
		{"diff", "--binary", "--no-ext-diff"},
		{"diff", "--cached", "--binary", "--no-ext-diff"},
	}
	for _, args := range commands {
		if err := hashGitOutput(ctx, hash, absRoot, args...); err != nil {
			return "", err
		}
		_, _ = hash.Write([]byte{0xff})
	}
	untracked, err := gitOutput(ctx, absRoot, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", err
	}
	for _, raw := range bytes.Split(untracked, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		rel := filepath.FromSlash(string(raw))
		target := filepath.Join(absRoot, rel)
		if !within(target, absRoot) {
			return "", fmt.Errorf("Git 返回越界 untracked 路径 %q", raw)
		}
		file, info, _, openErr := openHostFileNoFollow(target)
		if openErr != nil {
			return "", openErr
		}
		_, _ = hash.Write(raw)
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(fmt.Sprintf("%d", info.Mode())))
		if _, copyErr := io.Copy(hash, file); copyErr != nil {
			_ = file.Close()
			return "", copyErr
		}
		if closeErr := file.Close(); closeErr != nil {
			return "", closeErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func runGitDiscard(ctx context.Context, root string, args ...string) error {
	_, err := gitOutput(ctx, root, args...)
	return err
}

func hashGitOutput(ctx context.Context, target io.Writer, root string, args ...string) error {
	commandArgs := append([]string{"-c", "core.quotepath=false", "-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	var stderr bytes.Buffer
	cmd.Stdout = target
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func gitOutput(ctx context.Context, root string, args ...string) ([]byte, error) {
	commandArgs := append([]string{"-c", "core.quotepath=false", "-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

var _ workcontract.RunCompletionGuard = (*GitChangeGuard)(nil)
