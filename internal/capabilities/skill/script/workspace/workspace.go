package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// PrepareLocalTask 创建本地任务型执行工作空间目录并返回契约。
func PrepareLocalTask(workspaceRoot, runID string) (execmodel.ExecutionWorkspace, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return execmodel.ExecutionWorkspace{}, fmt.Errorf("获取工作区失败: %w", err)
		}
		root = wd
	}
	id := strings.TrimSpace(runID)
	if id == "" {
		id = fmt.Sprintf("run-%d", os.Getpid())
	}
	base := filepath.Join(root, ".genesis", "runs", sanitize(id))
	work := filepath.Join(base, "work")
	input := filepath.Join(base, "input")
	output := filepath.Join(base, "output")
	tmp := filepath.Join(base, "tmp")
	for _, dir := range []string{work, input, output, tmp, filepath.Join(work, "skills")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return execmodel.ExecutionWorkspace{}, fmt.Errorf("创建执行工作空间失败: %w", err)
		}
	}
	return execmodel.ExecutionWorkspace{
		Mode:       execmodel.WorkspaceModeLocalTask,
		PathPolicy: execmodel.PathPolicyStrictWorkspace,
		WorkDir:    work,
		InputDir:   input,
		OutputDir:  output,
		TmpDir:     tmp,
		Metadata: map[string]string{
			"run_id":         id,
			"workspace_root": root,
		},
	}, nil
}

func sanitize(v string) string {
	v = strings.TrimSpace(v)
	replacer := strings.NewReplacer(`/`, `_`, `\`, `_`, `:`, `_`, `*`, `_`, `?`, `_`, `"`, `_`, `<`, `_`, `>`, `_`, `|`, `_`)
	return replacer.Replace(v)
}
