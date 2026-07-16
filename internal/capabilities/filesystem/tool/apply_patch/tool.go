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
		Name: "apply_patch",
		Description: "应用 Codex 格式补丁，支持 Add/Delete/Update/Move、多文件和多 hunk 修改。" +
			"✅ 推荐用 Add File 创建新文件（尤其是大文件/脚本）：patch 字段内容相比 write_file 的 content 字段，" +
			"不需要对 \\n、\\t 等做额外 JSON 转义（+ 前缀行是逐行写入），token 效率更高，截断风险更低。" +
			"适合：1) 局部、可审查的代码修改（Update File + hunk）；2) 创建中等到大型新文件（Add File）；" +
			"3) 删除或重命名文件（Delete/Move）。" +
			"仍受 max_tokens 限制，超大文件（>15000 字符）需分多次调用，先写骨架再 Update File 追加各段。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"patch": {Type: "string", Description: "以 *** Begin Patch 开始、*** End Patch 结束的 patch 文本；+ 前缀为新增行，- 前缀为删除行，空格前缀为上下文行"},
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
