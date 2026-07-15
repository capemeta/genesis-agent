package domain

import "time"

// FieldEvidence 画像字段提取来源证据
type FieldEvidence struct {
	SourceRunID     string    `json:"source_run_id"`
	SourceMessageID string    `json:"source_message_id"`
	ContentExcerpt  string    `json:"content_excerpt"` // 提取来源文本片段
	ExtractedAt     time.Time `json:"extracted_at"`
}

// UserProfileBuiltin 内置画像字段（强类型结构体，保证 IDE 提示与编译期检查）。
// 新增内置字段须修改本结构体并同步更新持久化 schema；自定义业务字段用 UserProfile.CustomFields。
type UserProfileBuiltin struct {
	Locale             string   `json:"locale"`              // BCP-47 语言标签，如 zh-CN / en-US
	CommunicationStyle string   `json:"communication_style"` // formal / casual / technical
	ToolPreferences    []string `json:"tool_preferences"`     // 偏好工具列表，如 ["bash", "python"]
	RiskPreference     string   `json:"risk_preference"`      // conservative / balanced / aggressive
	MemoryOptIn        bool     `json:"memory_opt_in"`        // 是否同意跨会话长期记忆（默认 true）
	Timezone           string   `json:"timezone"`             // IANA tz，如 Asia/Shanghai
	ResponseVerbosity  string   `json:"response_verbosity"`   // concise / normal / detailed
}

// UserProfile 用户画像（对齐 §11.8）。
// Builtin 字段强类型；CustomFields 保持 map 灵活度；两者独立持久化，互不影响。
type UserProfile struct {
	TenantID     string                   `json:"tenant_id"`
	UserID       string                   `json:"user_id"`
	Builtin      UserProfileBuiltin       `json:"builtin"`       // 内置字段（强类型，有 IDE 提示）
	CustomFields map[string]any           `json:"custom_fields"`  // 业务自定义字段（无 schema 约束）
	Evidence     map[string]FieldEvidence `json:"evidence"`       // 字段来源证据（用于可解释性）
	Confidence   map[string]float64       `json:"confidence"`    // 提取字段的置信度分值 (0~1)
	Visibility   map[string]string        `json:"visibility"`    // 字段可见性：user_visible/internal/system_only
	RuntimeAudit
}
