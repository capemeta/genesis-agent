package run_skill_script

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	"genesis-agent/internal/capabilities/skill/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/platform/contextutil"
)

// Deps 是 run_skill_script 工具依赖。
type Deps struct {
	Runner         scriptcontract.Runner
	CatalogRequest skillcontract.CatalogRequest
	Sandbox        execmodel.SandboxProfile
	WorkspaceRoot  string
}

// Tool 执行已 materialize 的 Skill 脚本。
type Tool struct {
	deps Deps
}

type input struct {
	Skill     string   `json:"skill"`
	Script    string   `json:"script"`
	Args      []string `json:"args,omitempty"`
	Inputs    []string `json:"inputs,omitempty"`
	TimeoutMS int64    `json:"timeout_ms,omitempty"`
}

// New 创建 run_skill_script 工具。
func New(deps Deps) (tool.Tool, error) {
	if deps.Runner == nil {
		return nil, fmt.Errorf("skill script runner未配置")
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name: "run_skill_script",
		Description: strings.TrimSpace(`
执行 Skill 包内 scripts/ 下的业务脚本（内置 embed 与用户磁盘 Skill 同一入口）。
参数 script 必须是可执行业务脚本的 resource id，例如 office-ppt/scripts/inspect_pptx.py 或 office-ppt/scripts/render_pptx_preview.py。
禁止把 path_contract.py、__init__.py 等辅助模块当作 script 入口。
运行时会 materialize 脚本、注入 INPUT_DIR/OUTPUT_DIR/SKILL_DIR，并按 workload 选择 sandbox profile。
禁止用 write_file 伪造 .pptx/.docx/.xlsx/.pdf；交付物由脚本写入 OUTPUT_DIR 并经格式门禁校验。
注意：office-ppt 从零生成默认走 run_pptxgen_script.js（Agent 写顶层 pptxgenjs 脚本后由 runner 执行）；create_pptx.js 仅 smoke。另有 inspect/preview/thumbnail/add_slide/clean/unpack/pack。不要臆造不存在的脚本能力；以 list_skill_resources 为准。
`),
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"skill":      {Type: "string", Description: "Skill 名称，例如 office-ppt"},
				"script":     {Type: "string", Description: "脚本 resource id，例如 office-ppt/scripts/inspect_pptx.py"},
				"args":       {Type: "array", Description: "传给脚本的参数列表（文件名相对 INPUT_DIR）", Items: &tool.ParameterSchema{Type: "string"}},
				"inputs":     {Type: "array", Description: "输入路径：工作区相对路径、本 Run 产物文件名，或 $OUTPUT_DIR/$INPUT_DIR/...；会 stage 到 INPUT_DIR", Items: &tool.ParameterSchema{Type: "string"}},
				"timeout_ms": {Type: "integer", Description: "超时毫秒，默认 120000"},
			},
			Required: []string{"skill", "script"},
		},
		Traits: tool.ToolTraits{
			Exposure:        tool.ToolExposureDirect,
			ReadOnly:        false,
			ConcurrencySafe: false,
			NeedsPermission: true,
		},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := json.Unmarshal([]byte(params), &in); err != nil {
		return "", fmt.Errorf("解析run_skill_script参数失败: %w", err)
	}
	skill := strings.TrimSpace(in.Skill)
	script := strings.TrimSpace(in.Script)
	if skill == "" || script == "" {
		return "", fmt.Errorf("skill与script不能为空")
	}
	runID := ""
	if id, ok := contextutil.GetRunID(ctx); ok {
		runID = id
	}
	result, err := t.deps.Runner.Run(ctx, scriptcontract.RunRequest{
		Catalog:       t.deps.CatalogRequest,
		Skill:         skill,
		Script:        model.ResourceID(script),
		Args:          in.Args,
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
	// ok=false 必须以错误返回，避免 runtime 记「工具执行成功」误导模型重试假成功路径。
	if result != nil && !result.OK {
		msg := strings.TrimSpace(result.Error)
		if msg == "" {
			msg = "run_skill_script failed"
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
