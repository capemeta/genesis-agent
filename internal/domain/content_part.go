package domain

import "strings"

// ContentPartType 消息内容分片类型。
type ContentPartType string

const (
	ContentPartText  ContentPartType = "text"
	ContentPartImage ContentPartType = "image"
)

// ImageRef 是可持久化的图片引用（禁止承载原始 bytes / base64）。
type ImageRef struct {
	CandidateID        string `json:"candidate_id,omitempty"`
	ProducedResourceID string `json:"produced_resource_id,omitempty"`
	PathAlias          string `json:"path_alias,omitempty"` // workspace-relative
	MediaType          string `json:"media_type,omitempty"`
	SHA256             string `json:"sha256,omitempty"`
	Width              int    `json:"width,omitempty"`
	Height             int    `json:"height,omitempty"`
	Detail             string `json:"detail,omitempty"` // low | high | auto
	AttachmentID       string `json:"attachment_id,omitempty"`
	// LocalReadPath 仅进程内 outbound 物化使用，禁止 JSON/DB 持久化。
	LocalReadPath string `json:"-"`
	// InlineBytes 仅进程内 ephemeral 物化（如 candidate_id 读出后），禁止持久化。
	InlineBytes []byte `json:"-"`
}

// ContentPart 是 Message 的多模态分片。
type ContentPart struct {
	Type     ContentPartType `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageRef *ImageRef       `json:"image_ref,omitempty"`
}

// TextContent 返回可供旧路径使用的纯文本：优先拼接 text parts，否则回退 Content。
func (m *Message) TextContent() string {
	if m == nil {
		return ""
	}
	if len(m.Parts) == 0 {
		return m.Content
	}
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Type == ContentPartText && p.Text != "" {
			b.WriteString(p.Text)
		}
	}
	if s := b.String(); s != "" {
		return s
	}
	return m.Content
}

// HasImageParts 是否包含未剥离的 image part。
func (m *Message) HasImageParts() bool {
	if m == nil {
		return false
	}
	for _, p := range m.Parts {
		if p.Type == ContentPartImage && p.ImageRef != nil {
			return true
		}
	}
	return false
}

// NewUserMessageWithParts 创建带 Parts 的用户消息；Content 填文本聚合便于兼容。
func NewUserMessageWithParts(text string, parts []ContentPart) *Message {
	msg := &Message{
		Role:    RoleUser,
		Content: text,
		Kind:    MessageKindUserTurn,
		Source:  MessageSourceUser,
		Parts:   parts,
	}
	if text == "" && len(parts) > 0 {
		msg.Content = msg.TextContent()
	}
	return msg
}
