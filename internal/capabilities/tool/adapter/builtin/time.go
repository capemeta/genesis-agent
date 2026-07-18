// Package builtin 提供内置工具实现
package builtin

import (
	"context"
	"fmt"
	"time"

	"genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
)

// CurrentTimeTool 获取当前时间的工具
type CurrentTimeTool struct{}

// NewCurrentTimeTool 创建当前时间工具
func NewCurrentTimeTool() tool.Tool {
	return &CurrentTimeTool{}
}

func (t *CurrentTimeTool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "current_time",
		Description: "获取当前的日期和时间。当用户询问时间相关问题时使用此工具。",
		Parameters: &tool.ParameterSchema{
			Type:       "object",
			Properties: map[string]*tool.ParameterSchema{},
			Required:   []string{},
		},
	}
}

func (t *CurrentTimeTool) Execute(_ context.Context, params string) (string, error) {
	if err := toolparam.DecodeOptional(params, &struct{}{}); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	now := time.Now()
	return fmt.Sprintf("%s（时区：%s）", now.Format("2006年01月02日 15:04:05"), now.Location().String()), nil
}
