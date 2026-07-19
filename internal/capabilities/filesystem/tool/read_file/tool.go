// Package read_file 实现 read_file 工具。
package read_file

import (
	"context"
	"path"
	"strings"

	"genesis-agent/internal/capabilities/filesystem/binarygate"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
)

// Tool 读取文件。
type Tool struct {
	deps toolkit.Deps
}

type input struct {
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

type output struct {
	Path             string `json:"path"`
	Content          string `json:"content,omitempty"`
	Size             int64  `json:"size,omitempty"`
	Hash             string `json:"hash,omitempty"`
	Truncated        bool   `json:"truncated,omitempty"`
	Binary           bool   `json:"binary,omitempty"`
	MIME             string `json:"mime,omitempty"`
	Message          string `json:"message,omitempty"`
	Error            string `json:"error,omitempty"`
	FailureKind      string `json:"failure_kind,omitempty"`
	SuggestedAction  string `json:"suggested_action,omitempty"`
}

// New 创建 read_file 工具。
func New(deps toolkit.Deps) (tool.Tool, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "read_file",
		Description: "读取当前 workspace 内或经审批的外部文本文件。必须提供 path，可用 max_bytes 限制最大读取字节数。二进制文件只返回元信息，不返回原始内容。远程 Skill cwd 中的预览图对宿主路径不可见，请继续用 run_skill_command 检查。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"path":      {Type: "string", Description: "workspace 内或经审批的外部文件路径"},
				"max_bytes": {Type: "integer", Description: "最大读取字节数，省略时使用默认限制"},
			},
			Required: []string{"path"},
		},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolkit.DecodeParams(params, &in); err != nil {
		return "", err
	}
	pathRef, err := toolkit.ResolveRequire(ctx, t.deps, "read_file", in.Path, permission.OperationRead, fscontract.ResolveOptions{
		Operation: string(permission.OperationRead),
		MustExist: true,
	})
	if err != nil {
		if guidance, ok := missingImageGuidance(in.Path, err); ok {
			return toolkit.ToJSON(guidance)
		}
		return "", err
	}
	release, err := toolkit.Acquire(ctx, t.deps.Locker, []scheduler.ResourceLock{{
		Scope: "file",
		Key:   toolkit.FileLockKey(pathRef),
		Mode:  scheduler.LockRead,
	}})
	if err != nil {
		return "", err
	}
	defer release()

	data, readErr := t.deps.Backend.Read(ctx, pathRef, fscontract.ReadOptions{MaxBytes: in.MaxBytes})
	truncated := fscontract.CodeOf(readErr) == fscontract.ErrCodeTooLarge
	if readErr != nil && !truncated {
		if guidance, ok := missingImageGuidance(in.Path, readErr); ok {
			return toolkit.ToJSON(guidance)
		}
		return "", readErr
	}
	stat, err := t.deps.Backend.Stat(ctx, pathRef)
	if err != nil {
		return "", err
	}
	hash := toolkit.HashBytes(data)
	if !truncated {
		if err := t.deps.Freshness.RecordRead(ctx, pathRef, *stat, hash); err != nil {
			return "", err
		}
	}
	class := binarygate.ClassifyContent(pathRef.DisplayPath, data)
	out := output{
		Path:      pathRef.DisplayPath,
		Size:      stat.Size,
		Hash:      hash,
		Truncated: truncated,
	}
	if class.Binary {
		out.Binary = true
		out.MIME = class.MIME
		out.Message = binaryReadMessage(pathRef.DisplayPath, class.MIME)
		out.SuggestedAction = "use_run_skill_command_for_skill_cwd_or_await_view_image"
		return toolkit.ToJSON(out)
	}
	out.Content = string(data)
	return toolkit.ToJSON(out)
}

func missingImageGuidance(requestPath string, err error) (output, bool) {
	if fscontract.CodeOf(err) != fscontract.ErrCodeNotFound {
		return output{}, false
	}
	if !looksLikeImagePath(requestPath) {
		return output{}, false
	}
	return output{
		Path:            requestPath,
		Error:           string(fscontract.ErrCodeNotFound),
		FailureKind:     "host_path_missing",
		SuggestedAction: "use_run_skill_command_for_skill_cwd",
		Message: "宿主 workspace 找不到该图片。若文件由 remote_sandbox/run_skill_command 生成，它位于技能 cwd 而非宿主 binding；" +
			"请继续用 run_skill_command 查看/QA。produced[] 中 role=qa_asset 的 candidate_id 仅作资源身份，当前勿对 jpg/png 使用宿主 read_file。",
	}, true
}

func looksLikeImagePath(p string) bool {
	switch strings.ToLower(path.Ext(strings.TrimSpace(p))) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".ico":
		return true
	default:
		return false
	}
}

func binaryReadMessage(displayPath, mimeType string) string {
	if looksLikeImagePath(displayPath) || strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return "read_file 不返回图片像素。远程 Skill 预览图请用 run_skill_command 在技能 cwd 内检查；" +
			"produced[] 若含 role=qa_asset 仅表示已登记 leased 资源，宿主 read_file 仍不可见。"
	}
	return "read_file只返回文本内容；该文件是二进制，content已省略。请使用对应的文档/媒体检查工具处理该产物。"
}
