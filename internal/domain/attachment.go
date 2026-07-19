package domain

// AttachmentRole 附件在 Turn 中的分流角色。
type AttachmentRole string

const (
	AttachmentRoleImage    AttachmentRole = "image"
	AttachmentRoleDocument AttachmentRole = "document"
	AttachmentRoleOther    AttachmentRole = "other"
)

// AttachmentSource 附件采集来源（产品面）。
type AttachmentSource string

const (
	AttachmentSourceUpload         AttachmentSource = "upload"
	AttachmentSourceWorkspace      AttachmentSource = "workspace"
	AttachmentSourceClipboard      AttachmentSource = "clipboard"
	AttachmentSourceProjectPicker  AttachmentSource = "project_picker"
	AttachmentSourceCLIAttach      AttachmentSource = "cli_attach"
)

// AttachmentDescriptor 是进入 Engine 的统一附件契约（不含原始 bytes）。
type AttachmentDescriptor struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	MIME           string           `json:"mime"`
	SHA256         string           `json:"sha256,omitempty"`
	Size           int64            `json:"size,omitempty"`
	Role           AttachmentRole   `json:"role"`
	Source         AttachmentSource `json:"source,omitempty"`
	Width          int              `json:"width,omitempty"`
	Height         int              `json:"height,omitempty"`
	ExtractedText  string           `json:"extracted_text,omitempty"`
	// ContentBase64 仅允许图片（jpeg/png/webp/gif）在发消息/StartRun 时内联；
	// 服务端入站后必须立即 staging 并清空，不得持久化到 Message Store。
	// 文档/音视频/压缩包请先走 POST /files 拿 id，再在 StartRun 只带标识。
	ContentBase64 string `json:"content_base64,omitempty"`
	WorkspaceAlias string           `json:"workspace_alias,omitempty"`
	// InputRef 是 staging 后的不可变输入标识（跨端协议字段；不含 bytes）。
	InputRef *AttachmentInputRef `json:"input_ref,omitempty"`
	// LocalPath 仅本地产品适配器瞬态使用，不得跨端序列化为协议字段；JSON 省略。
	LocalPath string `json:"-"`
}

// AttachmentInputRef 是 Descriptor 上的轻量 InputRef 投影（避免 domain 依赖 workspace）。
type AttachmentInputRef struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	Alias      string `json:"alias,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	MIME       string `json:"mime,omitempty"`
	StagedPath string `json:"staged_path,omitempty"`
	Size       int64  `json:"size,omitempty"`
}
