// Package write_stdin 提供向活动命令行 PTY 会话标准输入发送数据的工具。
package write_stdin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

// Deps 是 write_stdin 工具的依赖项。
type Deps struct {
	SessionManager execcontract.InteractiveSessionRunner
}

// Validate 校验依赖。
func (d Deps) Validate() error {
	if d.SessionManager == nil {
		return fmt.Errorf("InteractiveSessionRunner未配置")
	}
	return nil
}

// Tool 向 PTY 会话写入 stdin 字符。
type Tool struct {
	deps Deps
}

type input struct {
	SessionID string `json:"session_id"`
	Input     string `json:"input"`
}

// New 创建 write_stdin 工具。
func New(deps Deps) (tool.Tool, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "write_stdin",
		Description: "向指定的活动交互式命令行（PTY）会话中写入键盘输入字符。用于回答需要二次应答的密码、认证或确认问答。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"session_id": {Type: "string", Description: "目标 PTY 交互会话 ID"},
				"input":      {Type: "string", Description: "要送入物理控制台的文本字符（如 yes\\n ）"},
			},
			Required: []string{"session_id", "input"},
		},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := decodeParams(params, &in); err != nil {
		return "", err
	}

	in.SessionID = strings.TrimSpace(in.SessionID)
	if in.SessionID == "" {
		return "", execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("session_id 不能为空"))
	}

	// 物理送入 SessionManager
	err := t.deps.SessionManager.WriteStdin(ctx, in.SessionID, []byte(in.Input))
	if err != nil {
		return "", err
	}

	res := map[string]any{
		"session_id":    in.SessionID,
		"status":        "success",
		"written_bytes": len(in.Input),
	}
	data, _ := json.MarshalIndent(res, "", "  ")
	return string(data), nil
}

func decodeParams(params string, dst any) error {
	decoder := json.NewDecoder(strings.NewReader(params))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("参数解析失败: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("参数只能包含一个JSON对象")
	}
	return nil
}
