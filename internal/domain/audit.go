package domain

import "time"

// ResourceAudit 资源表标准审计字段
// 用于：agent / agent_instance / agent_session / agent_task / agent_webhook
type ResourceAudit struct {
	OwnerID   string // 当前拥有者（可转让，权限控制）
	OwnerName string // 拥有者名称冗余，避免 JOIN
	CreatedBy string // 最初创建人 user_id，不可变
	UpdatedBy string // 最后修改人 user_id
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RuntimeAudit 运行时流水表标准审计字段
// 用于：agent_run / agent_step / agent_intervention / agent_memory / agent_usage
type RuntimeAudit struct {
	OwnerID   string // 继承自父实体，用于按用户过滤与报表
	OwnerName string // 仅 run / usage 等报表表使用，其他表可为空
	CreatedAt time.Time
	UpdatedAt time.Time // event / message 等只写表在 DB 无此列，领域层为零值
}
