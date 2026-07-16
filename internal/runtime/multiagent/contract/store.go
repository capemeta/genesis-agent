package contract

import (
	"context"

	"genesis-agent/internal/runtime/multiagent/model"
)

// StoredInstance 是可恢复的子智能体控制面快照；不包含 transcript 或工具轨迹。
type StoredInstance struct {
	Instance model.Instance
	Request  SpawnRequest
}

// InstanceStore 是产品注入的子智能体实例存储端口。
type InstanceStore interface {
	Save(ctx context.Context, value StoredInstance) error
	Get(ctx context.Context, agentID string) (StoredInstance, error)
}

// ResultDeliveryKey 标识一次父 Run 对子任务终态结果的模型上下文消费。
type ResultDeliveryKey struct {
	TenantID    string
	SessionID   string
	ParentRunID string
	AgentID     string
	ResultID    string
}

// ResultDeliveryStore 记录 TaskOutput 是否已把某个 result_id 注入过父上下文。
type ResultDeliveryStore interface {
	MarkDelivered(ctx context.Context, key ResultDeliveryKey) (alreadyDelivered bool, err error)
}
