package run_skill_command

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/platform/contextutil"
)

// Deps 是 run_skill_command 工具依赖。
type Deps struct {
	Runner         scriptcontract.Runner
	CatalogRequest skillcontract.CatalogRequest
	Sandbox        execmodel.SandboxProfile
	WorkspaceRoot  string
}

type Tool struct {
	deps Deps
}

type input struct {
	Skill     string   `json:"skill"`
	Command   string   `json:"command"`
	Inputs    []string `json:"inputs,omitempty"`
	TimeoutMS int64    `json:"timeout_ms,omitempty"`
}

func New(deps Deps) (tool.Tool, error) {
	if deps.Runner == nil {
		return nil, fmt.Errorf("skill command runner未配置")
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name: "run_skill_command",
		Description: strings.TrimSpace(`
在当前 Skill 的持久工作目录中按原文执行命令。
当第三方 SKILL.md 写 python scripts/foo.py、python -m bar、node scripts/foo.js、pdftoppm ... 时，应直接把整条命令放进 command，由运行时负责 materialize skill、准备工作目录、注入环境并选择合适的 sandbox/profile。
需要执行 JS/Python 时，默认写入 $WORK_DIR 脚本再 python foo.py / node foo.js；禁止 python -c / node -e 多行或长串内联（Windows/远程 shell 引号易失败）。仅极短单行探测可例外。
inputs 可选，语义是控制面 stage 源：用 $WORK_DIR/... 或工作区相对路径；禁止 /workspace/... 等执行面绝对路径。若提供，会在执行前复制到当前 Skill 工作目录，供命令用相对文件名访问；文件已在工作目录或仅跑包内脚本时请省略。
command 写相对文件名或包内 scripts/...；禁止把 $WORK_DIR/$INPUT_DIR/$OUTPUT_DIR/$TMPDIR/$SKILL_DIR 写进 command（本地与远程 sandbox 均不展开）。正确：inputs=["$WORK_DIR/foo.py"] + command="python foo.py"。
返回 metadata.execution_backend / degraded；首次或环境变化时含 path_map（勿把右侧路径写入 inputs/command）。
交付物不要用 write_file 伪造 .pptx/.docx/.xlsx/.pdf；应由命令真实生成。
`),
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"skill":      {Type: "string", Description: "Skill 名称，例如 office-ppt"},
				"command":    {Type: "string", Description: "在 Skill 工作目录执行的命令，例如 python create_pdfs.py；禁止含 $WORK_DIR；避免多行 python -c / node -e"},
				"inputs":     {Type: "array", Description: "可选控制面路径：stage 到 Skill 工作目录。用 $WORK_DIR/foo 或工作区相对路径；禁止 /workspace/...；已在 cwd 则省略", Items: &tool.ParameterSchema{Type: "string"}},
				"timeout_ms": {Type: "integer", Description: "超时毫秒，默认 120000"},
			},
			Required: []string{"skill", "command"},
		},
		Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: false, NeedsPermission: true},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := json.Unmarshal([]byte(params), &in); err != nil {
		return "", fmt.Errorf("解析run_skill_command参数失败: %w", err)
	}
	skill := strings.TrimSpace(in.Skill)
	command := strings.TrimSpace(in.Command)
	if skill == "" || command == "" {
		return "", fmt.Errorf("skill与command不能为空")
	}
	runID := ""
	if id, ok := contextutil.GetRunID(ctx); ok {
		runID = id
	}
	result, err := t.deps.Runner.Run(ctx, scriptcontract.RunRequest{
		Catalog:       t.deps.CatalogRequest,
		Skill:         skill,
		Command:       command,
		Inputs:        in.Inputs,
		RunID:         runID,
		TimeoutMS:     in.TimeoutMS,
		Sandbox:       cloneSandbox(t.deps.Sandbox),
		WorkspaceRoot: t.deps.WorkspaceRoot,
	})
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	if result != nil && !result.OK {
		msg := strings.TrimSpace(result.Error)
		if msg == "" {
			msg = "run_skill_command failed"
		}
		return string(data), fmt.Errorf("%s", msg)
	}
	return string(data), nil
}

func cloneSandbox(in execmodel.SandboxProfile) execmodel.SandboxProfile {
	out := in
	if in.Metadata != nil {
		out.Metadata = make(map[string]string, len(in.Metadata))
		for k, v := range in.Metadata {
			out.Metadata[k] = v
		}
	}
	return out
}
