// Package apply_patch 实现 Codex 风格 apply_patch 工具。
package apply_patch

import (
	"context"
	"fmt"

	"genesis-agent/internal/capabilities/filesystem/patch"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

// Tool 应用结构化补丁。
type Tool struct {
	service *patch.Service
}

type input struct {
	Patch string `json:"patch"`
}

// New 创建 apply_patch 工具。
func New(deps toolkit.Deps) (tool.Tool, error) {
	service, err := patch.NewService(deps)
	if err != nil {
		return nil, err
	}
	return &Tool{service: service}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "apply_patch",
		Description: "应用 Codex 风格补丁，支持 Add/Delete/Update/Move、多文件和多 hunk 修改。复杂代码修改优先使用本工具。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"patch": {Type: "string", Description: "以 *** Begin Patch 开始、*** End Patch 结束的 patch 文本"},
			},
			Required: []string{"patch"},
		},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolkit.DecodeParams(params, &in); err != nil {
		return "", err
	}
	if in.Patch == "" {
		return "", fmt.Errorf("patch不能为空")
	}
	result, err := t.service.Apply(ctx, in.Patch)
	if err != nil {
		return "", err
	}
	return toolkit.ToJSON(result)
}
