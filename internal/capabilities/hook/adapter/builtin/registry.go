// Package builtin 提供受信任的进程内 Hook handler 注册表。
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"genesis-agent/internal/capabilities/hook/model"
)

type Handler func(context.Context, []byte) model.Decision

// Registry 是并发安全的 builtin handler 注册表。
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

func NewRegistry() *Registry   { return &Registry{handlers: make(map[string]Handler)} }
func (*Registry) Kind() string { return "builtin" }

// NewDefaultRegistry 注册随内核发布的保守安全守卫。
func NewDefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register("git_branch_guard", gitBranchGuard)
	r.Register("secret_path_guard", secretPathGuard)
	return r
}
func (r *Registry) Register(name string, handler Handler) {
	if r != nil && name != "" && handler != nil {
		r.mu.Lock()
		r.handlers[name] = handler
		r.mu.Unlock()
	}
}
func (r *Registry) Run(ctx context.Context, spec model.HandlerSpec, input []byte) model.Decision {
	if r == nil {
		return model.Decision{Continue: true, Err: fmt.Errorf("builtin Hook registry未配置")}
	}
	r.mu.RLock()
	handler := r.handlers[spec.Builtin]
	r.mu.RUnlock()
	if handler == nil {
		return model.Decision{Continue: true, Err: fmt.Errorf("未注册 builtin Hook %q", spec.Builtin)}
	}
	return handler(ctx, input)
}

func gitBranchGuard(_ context.Context, input []byte) model.Decision {
	if truthy(os.Getenv("GENESIS_ALLOW_GIT_BRANCH_SWITCH")) {
		return model.Decision{Continue: true}
	}
	payload := decodePayload(input)
	if !isCommandTool(payload) {
		return model.Decision{Continue: true}
	}
	command := strings.ToLower(extractString(payload, "tool_input", "command"))
	if strings.Contains(command, "git switch") || (strings.Contains(command, "git checkout") && !strings.Contains(command, " -- ")) {
		return model.Decision{Continue: false, Reason: "内置 git_branch_guard 阻止可能切换分支的 Git 操作；请设置 GENESIS_ALLOW_GIT_BRANCH_SWITCH=1 后重试"}
	}
	return model.Decision{Continue: true}
}

func secretPathGuard(_ context.Context, input []byte) model.Decision {
	payload := decodePayload(input)
	toolName, _ := payload["tool_name"].(string)
	switch toolName {
	case "read_file", "grep", "glob", "walk_dir", "list_dir":
	default:
		return model.Decision{Continue: true}
	}
	encoded, _ := json.Marshal(payload["tool_input"])
	path := strings.ToLower(string(encoded))
	for _, marker := range []string{".env", ".ssh", "id_rsa", "credentials", ".aws"} {
		if strings.Contains(path, marker) {
			return model.Decision{Continue: false, Reason: "内置 secret_path_guard 阻止访问可能包含凭据的路径"}
		}
	}
	return model.Decision{Continue: true}
}

func decodePayload(input []byte) map[string]any {
	var payload map[string]any
	if json.Unmarshal(input, &payload) != nil {
		return map[string]any{}
	}
	return payload
}

func isCommandTool(payload map[string]any) bool {
	name, _ := payload["tool_name"].(string)
	return name == "run_command" || name == "run_skill_command"
}

func extractString(payload map[string]any, objectKey, key string) string {
	object, _ := payload[objectKey].(map[string]any)
	value, _ := object[key].(string)
	return value
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
