package model

import (
	"fmt"
	"strings"
)

// MCPFileBindingKind 描述声明字段的资源语义。
type MCPFileBindingKind string

const (
	MCPFileBindingInputRef MCPFileBindingKind = "input_ref"
	MCPFileBindingArtifact MCPFileBindingKind = "artifact"
)

// MCPFileBinding 只改写明确声明的 JSON Pointer 字段。
type MCPFileBinding struct {
	JSONPointer string             `json:"json_pointer"`
	Kind        MCPFileBindingKind `json:"kind"`
}

func (b MCPFileBinding) Validate() error {
	if !strings.HasPrefix(b.JSONPointer, "/") || strings.ContainsAny(b.JSONPointer, "\x00\r\n") {
		return fmt.Errorf("非法 MCP file binding json_pointer: %q", b.JSONPointer)
	}
	switch b.Kind {
	case MCPFileBindingInputRef, MCPFileBindingArtifact:
		return nil
	default:
		return fmt.Errorf("未知 MCP file binding kind: %q", b.Kind)
	}
}
