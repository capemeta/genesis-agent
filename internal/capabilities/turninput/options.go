package turninput

import "strings"

// DocumentExtractMode 控制 E1 文档是否在首轮 LLM 前预抽正文。
type DocumentExtractMode string

const (
	DocumentExtractPreview  DocumentExtractMode = "preview"
	DocumentExtractPathOnly DocumentExtractMode = "path_only"
	DocumentExtractOff      DocumentExtractMode = "off"
)

// MentionResolveMode 控制 E2 文本点名是否解析工作区路径。
type MentionResolveMode string

const (
	MentionResolveOff        MentionResolveMode = "off"
	MentionResolveHint       MentionResolveMode = "hint"
	MentionResolveAutoAttach MentionResolveMode = "auto_attach"
)

// Options 是 Turn 装配开关（产品 Profile / bootstrap 注入）。
type Options struct {
	DocumentExtract DocumentExtractMode
	MentionResolve  MentionResolveMode
	WorkspaceRoot   string
}

// NormalizeDocumentExtract 归一化；空值视为 path_only（Coding 安全默认）。
func NormalizeDocumentExtract(m DocumentExtractMode) DocumentExtractMode {
	switch DocumentExtractMode(strings.ToLower(strings.TrimSpace(string(m)))) {
	case DocumentExtractPreview:
		return DocumentExtractPreview
	case DocumentExtractOff, DocumentExtractPathOnly:
		return DocumentExtractPathOnly
	default:
		return DocumentExtractPathOnly
	}
}

// ShouldExtractDocuments 是否执行预抽（preview）。
func ShouldExtractDocuments(m DocumentExtractMode) bool {
	return NormalizeDocumentExtract(m) == DocumentExtractPreview
}

// NormalizeMentionResolve 归一化；空值视为 off。
func NormalizeMentionResolve(m MentionResolveMode) MentionResolveMode {
	switch MentionResolveMode(strings.ToLower(strings.TrimSpace(string(m)))) {
	case MentionResolveHint:
		return MentionResolveHint
	case MentionResolveAutoAttach:
		return MentionResolveAutoAttach
	default:
		return MentionResolveOff
	}
}

// DefaultOptionsForProduct 按产品给出推荐默认。
func DefaultOptionsForProduct(product string) Options {
	switch strings.ToLower(strings.TrimSpace(product)) {
	case "enterprise":
		return Options{DocumentExtract: DocumentExtractPreview, MentionResolve: MentionResolveOff}
	default:
		// cli / desktop / coding：对齐 Codex path_only
		return Options{DocumentExtract: DocumentExtractPathOnly, MentionResolve: MentionResolveOff}
	}
}
