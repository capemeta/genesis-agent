package tooladapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

// mcpTool 把一个 MCP server tool 适配为 genesis-agent 的 tool.Tool。
// 不缓存 Session（会因健康检查/重连失效），执行时通过 Manager 解析。
type mcpTool struct {
	manager    contract.Manager
	serverName string
	toolName   string
	modelName  string
	info       *tool.Info
	timeout    time.Duration
	mu         sync.RWMutex
}

// New 创建 MCP tool 投影。
func New(manager contract.Manager, serverName, originalTool, modelName string, snap model.ToolSnapshot, exposure tool.ToolExposure, timeout time.Duration) tool.Tool {
	readOnly := false
	if snap.ReadOnlyHint != nil {
		readOnly = *snap.ReadOnlyHint
	}
	desc := strings.TrimSpace(snap.Description)
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %s from server %s", originalTool, serverName)
	}
	info := tool.WithTraits(&tool.Info{
		Name:        modelName,
		Description: desc,
		Parameters:  ConvertInputSchema(snap.InputSchema),
	}, tool.ToolTraits{
		Exposure:        exposure,
		ReadOnly:        readOnly,
		NeedsPermission: true,
		ConcurrencySafe: readOnly,
	})
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &mcpTool{
		manager:    manager,
		serverName: serverName,
		toolName:   originalTool,
		modelName:  modelName,
		info:       info,
		timeout:    timeout,
	}
}

func (t *mcpTool) GetInfo() *tool.Info {
	t.mu.RLock()
	defer t.mu.RUnlock()
	clone := *t.info
	return &clone
}

// SetExposure 实现 tool.ExposureUpdater，以受锁保护 MCP tool 的动态暴露状态。
func (t *mcpTool) SetExposure(exposure tool.ToolExposure) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.info.Traits.Exposure = exposure
}

func (t *mcpTool) Execute(ctx context.Context, params string) (string, error) {
	if t.manager == nil {
		return "", fmt.Errorf("mcp tool %s: manager 未初始化", t.modelName)
	}
	if err := t.manager.EnsureConnected(ctx, t.serverName); err != nil {
		return "", err
	}
	sess, ok := t.manager.SessionFor(t.serverName)
	if !ok || sess == nil {
		return "", fmt.Errorf("mcp server %q 未连接或不可用", t.serverName)
	}
	args := json.RawMessage(strings.TrimSpace(params))
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	callCtx := ctx
	var cancel context.CancelFunc
	if t.timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, t.timeout)
		defer cancel()
	}
	res, err := sess.CallTool(callCtx, t.toolName, args)
	if err != nil {
		return "", err
	}
	if res.IsError {
		return res.Content, fmt.Errorf("mcp tool %s 返回错误: %s", t.modelName, res.Content)
	}
	return res.Content, nil
}

var _ tool.ExposureUpdater = (*mcpTool)(nil)
