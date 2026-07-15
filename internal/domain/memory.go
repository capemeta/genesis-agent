package domain

import "time"

// MemoryScopeType 长期记忆隔离作用域类型
type MemoryScopeType string

const (
	MemoryScopeUser          MemoryScopeType = "user"           // 用户级：跨 Agent 共享
	MemoryScopeAgentInstance MemoryScopeType = "agent_instance" // Agent 实例级：Agent 私有
	MemoryScopeWorkspace     MemoryScopeType = "workspace"      // 工作区级：同工作区共享
	MemoryScopeProject       MemoryScopeType = "project"        // 项目级：经业务声明才跨 Agent 共享
)

// MemoryScope 记忆作用域标识，唯一确定一个隔离域
type MemoryScope struct {
	Type MemoryScopeType `json:"type"`
	ID   string          `json:"id"` // 对应 UserID / AgentInstanceID / WorkspaceID / ProjectID
}

// MemoryEntryType 长期记忆类型
type MemoryEntryType string

const (
	MemoryTypeEpisodic   MemoryEntryType = "episodic"   // 情节记忆：特定事件/交互经历
	MemoryTypeSemantic   MemoryEntryType = "semantic"   // 语义记忆：事实/领域知识
	MemoryTypeProcedural MemoryEntryType = "procedural" // 程序记忆：操作步骤/工作流偏好
	MemoryTypeNegative   MemoryEntryType = "negative"   // 负面记忆：失败教训/禁止事项
)

// LongTermEntry 长期记忆领域模型（演进自 MemoryEntry，补齐 scope/memory_type/confidence/sensitivity/decay）
type LongTermEntry struct {
	ID               string          `json:"id"`
	TenantID         string          `json:"tenant_id"`
	Scope            MemoryScope     `json:"scope"`             // 强类型作用域
	MemoryType       MemoryEntryType `json:"memory_type"`       // 强类型记忆类型
	Content          string          `json:"content"`
	CustomData       map[string]any  `json:"custom_data,omitempty"` // 业务自定义结构（符合 Schema 约束）
	Embedding        []float32       `json:"embedding,omitempty"`   // 向量存储后端写入；文件后端为空
	Importance       float64         `json:"importance"`        // 重要性权重 0~1
	Confidence       float64         `json:"confidence"`        // 置信度 0~1
	Status           string          `json:"status"`            // active / pending_review / superseded
	SensitivityLevel string          `json:"sensitivity_level"` // public/internal/confidential/secret/pii
	DecayPolicy      string          `json:"decay_policy"`      // none/time_decay/access_decay/custom
	Tags             []string        `json:"tags,omitempty"`
	SourceRunID      string          `json:"source_run_id,omitempty"`
	SourceMessageID  string          `json:"source_message_id,omitempty"`
	SupersedesID     string          `json:"supersedes_id,omitempty"` // 被本条目取代的旧条目 ID
	LastAccessedAt   time.Time       `json:"last_accessed_at"`
	ExpiredAt        *time.Time      `json:"expired_at,omitempty"`
	RuntimeAudit
}

// MemorySortKey 检索结果排序维度枚举
type MemorySortKey string

const (
	MemorySortByRelevance  MemorySortKey = "relevance"  // 纯语义相似度（向量后端）
	MemorySortByImportance MemorySortKey = "importance" // importance 字段权重（文件后端回退）
	MemorySortByRecency    MemorySortKey = "recency"    // 最近访问时间（动态记忆偏好）
	MemorySortByComposite  MemorySortKey = "composite"  // 推荐：复合得分（语义、重要性、时效）
)

// CompositeWeights 复合排序得分的权重参数。
// 默认值：w_rel=0.6, w_imp=0.3, w_rec=0.1
type CompositeWeights struct {
	Relevance  float64 `json:"relevance"`  // w_rel：语义相似度得分权重
	Importance float64 `json:"importance"` // w_imp：importance 字段权重
	Recency    float64 `json:"recency"`    // w_rec：时效衰减因子权重
}

// MemoryFilters 长期记忆的过滤条件
type MemoryFilters struct {
	MemoryTypes      []MemoryEntryType `json:"memory_types,omitempty"`
	MinImportance    float64           `json:"min_importance,omitempty"`
	SensitivityLevel string            `json:"sensitivity_level,omitempty"` // 过滤敏感度级别
}

// MemoryQuery LTM 检索请求
type MemoryQuery struct {
	Query            string            `json:"query"`
	Scopes           []MemoryScope     `json:"scopes"`
	TopK             int               `json:"top_k"`
	MinConfidence    float64           `json:"min_confidence"`
	Filters          MemoryFilters     `json:"filters"`
	SortBy           MemorySortKey     `json:"sort_by"` // 默认 MemorySortByComposite
	CompositeWeights *CompositeWeights `json:"composite_weights,omitempty"`
}


