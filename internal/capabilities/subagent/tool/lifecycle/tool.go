// Package lifecycle 提供后台子智能体的固定工具 TaskOutput 与 TaskStop。
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
)

type query struct {
	AgentID        string `json:"agent_id"`
	Block          bool   `json:"block,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type output struct {
	RetrievalStatus string            `json:"retrieval_status"`
	AgentID         string            `json:"agent_id"`
	Status          model.Status      `json:"status"`
	Result          *model.TaskResult `json:"result,omitempty"`
	ResultDelivered bool              `json:"result_delivered,omitempty"`
	DuplicateResult bool              `json:"duplicate_result,omitempty"`
}

type OutputTool struct {
	controller contract.Controller
	delivery   contract.ResultDeliveryStore
}
type StopTool struct {
	controller contract.Controller
	delivery   contract.ResultDeliveryStore
	cancels    contract.CancellationStore
}

type Option func(*options)

type options struct {
	delivery contract.ResultDeliveryStore
	cancels  contract.CancellationStore
}

func WithResultDeliveryStore(store contract.ResultDeliveryStore) Option {
	return func(opts *options) {
		opts.delivery = store
	}
}

func WithCancellationStore(store contract.CancellationStore) Option {
	return func(opts *options) {
		opts.cancels = store
	}
}

func New(controller contract.Controller, modifiers ...Option) (*OutputTool, *StopTool, error) {
	if controller == nil {
		return nil, nil, fmt.Errorf("subagent Controller不能为空")
	}
	opts := options{delivery: NewMemoryResultDeliveryStore()}
	for _, modifier := range modifiers {
		if modifier != nil {
			modifier(&opts)
		}
	}
	if opts.delivery == nil {
		return nil, nil, fmt.Errorf("ResultDeliveryStore不能为空")
	}
	return &OutputTool{controller: controller, delivery: opts.delivery}, &StopTool{controller: controller, delivery: opts.delivery, cancels: opts.cancels}, nil
}

func (t *OutputTool) GetInfo() *tool.Info {
	return outputInfo()
}
func (t *StopTool) GetInfo() *tool.Info { return info("TaskStop", "取消后台子智能体。") }
func info(name, description string) *tool.Info {
	return &tool.Info{Name: name, Description: description, Parameters: &tool.ParameterSchema{Type: "object", Properties: map[string]*tool.ParameterSchema{"agent_id": {Type: "string", Description: "Task 返回的 agent_id"}}, Required: []string{"agent_id"}}, Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: name == "TaskOutput", ConcurrencySafe: true, NeedsPermission: true}}
}
func outputInfo() *tool.Info {
	return &tool.Info{Name: "TaskOutput", Description: "获取后台子智能体的当前或最终结果。", Parameters: &tool.ParameterSchema{Type: "object", Properties: map[string]*tool.ParameterSchema{
		"agent_id":        {Type: "string", Description: "Task 返回的 agent_id"},
		"block":           {Type: "boolean", Description: "是否短暂等待终态"},
		"timeout_seconds": {Type: "integer", Description: "等待秒数，默认 30"},
	}, Required: []string{"agent_id"}}, Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: true, ConcurrencySafe: true, NeedsPermission: true}}
}
func (t *OutputTool) Execute(ctx context.Context, params string) (string, error) {
	var in query
	if err := json.Unmarshal([]byte(params), &in); err != nil {
		return "", fmt.Errorf("解析 TaskOutput 参数失败: %w", err)
	}
	instance, timedOut, err := t.waitForResult(ctx, in)
	if err != nil {
		return "", err
	}
	retrieval := "not_ready"
	if timedOut {
		retrieval = "timeout"
	}
	if instance.Result != nil {
		retrieval = "ready"
	}
	projected, err := t.projectResult(ctx, instance, retrieval)
	if err != nil {
		return "", err
	}
	return marshal(projected)
}
func (t *StopTool) Execute(ctx context.Context, params string) (string, error) {
	var in query
	if err := json.Unmarshal([]byte(params), &in); err != nil {
		return "", fmt.Errorf("解析 TaskStop 参数失败: %w", err)
	}
	instance, err := t.controller.Get(ctx, in.AgentID)
	if err != nil {
		return "", err
	}
	key, err := ownerKey(ctx, instance)
	if err != nil {
		return "", err
	}
	if instance.Status == model.StatusRunning && t.cancels != nil {
		if err := t.cancels.RequestStop(ctx, in.AgentID, key.ParentRunID); err != nil {
			return "", fmt.Errorf("记录 TaskStop 取消意图失败: %w", err)
		}
	} else {
		if err := t.controller.Stop(ctx, in.AgentID); err != nil {
			return "", err
		}
	}
	instance, err = t.controller.Get(ctx, in.AgentID)
	if err != nil {
		return "", err
	}
	retrieval := "not_ready"
	if instance.Result != nil {
		retrieval = "ready"
	}
	projected, err := t.projectResult(ctx, instance, retrieval)
	if err != nil {
		return "", err
	}
	return marshal(projected)
}

func (t *OutputTool) waitForResult(ctx context.Context, in query) (model.Instance, bool, error) {
	instance, err := t.controller.Get(ctx, in.AgentID)
	if err != nil {
		return instance, false, err
	}
	if _, err := ownerKey(ctx, instance); err != nil {
		return instance, false, err
	}
	if !in.Block || instance.Result != nil {
		return instance, false, err
	}
	timeout := time.Duration(in.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return instance, false, ctx.Err()
		case <-deadline.C:
			return instance, true, nil
		case <-ticker.C:
			instance, err = t.controller.Get(ctx, in.AgentID)
			if err != nil {
				return instance, false, err
			}
			if _, err := ownerKey(ctx, instance); err != nil {
				return instance, false, err
			}
			if instance.Result != nil {
				return instance, false, nil
			}
		}
	}
}

func ensureOwner(ctx context.Context, instance model.Instance) error {
	_, err := ownerKey(ctx, instance)
	return err
}

func (t *OutputTool) projectResult(ctx context.Context, instance model.Instance, retrieval string) (output, error) {
	if instance.Result == nil {
		return output{RetrievalStatus: retrieval, AgentID: instance.AgentID, Status: instance.Status}, nil
	}
	key, err := ownerKey(ctx, instance)
	if err != nil {
		return output{}, err
	}
	key.AgentID = instance.AgentID
	key.ResultID = instance.Result.ResultID
	if err := validateDeliveryKey(key); err != nil {
		return output{}, err
	}
	duplicate, err := t.delivery.MarkDelivered(ctx, key)
	if err != nil {
		return output{}, fmt.Errorf("记录 TaskOutput 结果交付失败: %w", err)
	}
	if duplicate {
		return output{RetrievalStatus: "ready", AgentID: instance.AgentID, Status: instance.Status, DuplicateResult: true}, nil
	}
	return output{RetrievalStatus: "ready", AgentID: instance.AgentID, Status: instance.Status, Result: instance.Result, ResultDelivered: true}, nil
}

func (t *StopTool) projectResult(ctx context.Context, instance model.Instance, retrieval string) (output, error) {
	return (&OutputTool{controller: t.controller, delivery: t.delivery}).projectResult(ctx, instance, retrieval)
}

func ownerKey(ctx context.Context, instance model.Instance) (contract.ResultDeliveryKey, error) {
	parentRunID, hasRun := contextutil.GetRunID(ctx)
	sessionID, hasSession := contextutil.GetSessionID(ctx)
	tenantID, hasTenant := contextutil.GetTenantID(ctx)
	if !hasRun || !hasSession || !hasTenant {
		return contract.ResultDeliveryKey{}, fmt.Errorf("Task 生命周期操作缺少调用方归属上下文")
	}
	if parentRunID != instance.ParentRunID || sessionID != instance.SessionID || tenantID != instance.TenantID {
		return contract.ResultDeliveryKey{}, fmt.Errorf("无权访问其他父 Run 的子智能体")
	}
	return contract.ResultDeliveryKey{TenantID: tenantID, SessionID: sessionID, ParentRunID: parentRunID}, nil
}

func marshal(value output) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
