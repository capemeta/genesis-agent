// Package turninput 将 Turn 附件分流并装配为 domain.Message Parts。
package turninput

import (
	"path/filepath"
	"strings"

	"genesis-agent/internal/domain"
	"genesis-agent/internal/capabilities/llm/vision"
)

// MaxExtractedTextBytes 文档预抽取文本写入上下文的上限。
const MaxExtractedTextBytes = 32 * 1024

// ClassifyMIME 按 MIME / 扩展名判定附件角色。
func ClassifyMIME(mime, name string) domain.AttachmentRole {
	m := strings.ToLower(strings.TrimSpace(mime))
	if strings.HasPrefix(m, "image/") {
		switch m {
		case "image/jpeg", "image/jpg", "image/png", "image/webp", "image/gif":
			return domain.AttachmentRoleImage
		default:
			// 其它 image/* 仍按 image 尝试；view_image 再校验
			return domain.AttachmentRoleImage
		}
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return domain.AttachmentRoleImage
	case ".docx", ".doc", ".xlsx", ".xls", ".pptx", ".ppt", ".pdf", ".txt", ".md", ".csv":
		return domain.AttachmentRoleDocument
	}
	switch {
	case strings.Contains(m, "officedocument"), strings.Contains(m, "msword"),
		strings.Contains(m, "ms-excel"), strings.Contains(m, "ms-powerpoint"),
		m == "application/pdf", strings.HasPrefix(m, "text/"):
		return domain.AttachmentRoleDocument
	}
	return domain.AttachmentRoleOther
}

// BuildUserTurnMessage 按视觉形态装配首轮用户消息。
func BuildUserTurnMessage(text string, attachments []domain.AttachmentDescriptor, mode vision.Mode) *domain.Message {
	parts := make([]domain.ContentPart, 0, 4)
	var docBuf strings.Builder
	var imageNotes strings.Builder

	for i := range attachments {
		att := attachments[i]
		if att.Role == "" {
			att.Role = ClassifyMIME(att.MIME, att.Name)
		}
		switch att.Role {
		case domain.AttachmentRoleImage:
			switch mode {
			case vision.ModeDirectInject:
				ref := &domain.ImageRef{
					PathAlias:     firstNonEmpty(att.WorkspaceAlias, att.Name),
					MediaType:     att.MIME,
					SHA256:        att.SHA256,
					Width:         att.Width,
					Height:        att.Height,
					AttachmentID:  att.ID,
					LocalReadPath: att.LocalPath,
				}
				parts = append(parts, domain.ContentPart{Type: domain.ContentPartImage, ImageRef: ref})
			case vision.ModeExpertRoute:
				imageNotes.WriteString("\n[image attachment queued for vision expert: ")
				imageNotes.WriteString(firstNonEmpty(att.WorkspaceAlias, att.Name))
				imageNotes.WriteString("]")
			default:
				imageNotes.WriteString("\n[image attachment present but vision_unavailable: ")
				imageNotes.WriteString(firstNonEmpty(att.WorkspaceAlias, att.Name))
				imageNotes.WriteString(" — do not invent image content via Pillow/pixel analysis; tell user visual understanding is unavailable]")
			}
		case domain.AttachmentRoleDocument:
			excerpt := att.ExtractedText
			if len(excerpt) > MaxExtractedTextBytes {
				excerpt = excerpt[:MaxExtractedTextBytes]
			}
			docBuf.WriteString("\n\n[document attachment: ")
			docBuf.WriteString(firstNonEmpty(att.WorkspaceAlias, att.Name))
			docBuf.WriteString("]")
			if excerpt != "" {
				docBuf.WriteString("\n")
				docBuf.WriteString(excerpt)
			}
		default:
			docBuf.WriteString("\n[file attachment: ")
			docBuf.WriteString(firstNonEmpty(att.WorkspaceAlias, att.Name))
			docBuf.WriteString("]")
		}
	}

	body := text
	if imageNotes.Len() > 0 {
		body += imageNotes.String()
	}
	if docBuf.Len() > 0 {
		body += docBuf.String()
	}
	if body != "" {
		parts = append([]domain.ContentPart{{Type: domain.ContentPartText, Text: body}}, parts...)
	}
	// direct_inject 时把 image parts 放在文本后（上面 append 顺序：先收集 image 再把 text 插到前）
	if mode == vision.ModeDirectInject {
		reordered := make([]domain.ContentPart, 0, len(parts))
		var images []domain.ContentPart
		for _, p := range parts {
			if p.Type == domain.ContentPartImage {
				images = append(images, p)
				continue
			}
			reordered = append(reordered, p)
		}
		reordered = append(reordered, images...)
		parts = reordered
	}
	return domain.NewUserMessageWithParts(body, parts)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
