// Package param 提供所有 Tool 共用的严格参数解析。
package param

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Decode 严格解析一个 JSON 对象：拒绝未知字段、空输入和尾随 JSON 值。
func Decode(raw string, dst any) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("参数不能为空")
	}
	return decode(raw, dst)
}

// DecodeOptional 严格解析可选 JSON 对象；空输入等价于空对象。
func DecodeOptional(raw string, dst any) error {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	return decode(raw, dst)
}

func decode(raw string, dst any) error {
	if dst == nil {
		return fmt.Errorf("参数目标不能为空")
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed[0] != '{' {
		return fmt.Errorf("参数必须是 JSON 对象")
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("参数解析失败: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("参数只能包含一个 JSON 值")
		}
		return fmt.Errorf("参数包含尾随内容: %w", err)
	}
	return nil
}
