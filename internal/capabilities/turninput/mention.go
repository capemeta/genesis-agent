package turninput

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"genesis-agent/internal/domain"
)

var mentionFileToken = regexp.MustCompile(`(?i)\b([a-z0-9][\w.-]*\.(?:png|jpe?g|gif|webp|pdf|docx?|xlsx?|pptx?|txt|md|csv))\b`)

// MentionResult 是 mention_resolve 的输出。
type MentionResult struct {
	Text        string
	Attachments []domain.AttachmentDescriptor
	Hints       []string
	Ambiguous   []string // mention_ambiguous
}

// ResolveMentions 按模式解析用户文本中的文件名点名。
// finder 可注入；nil 时使用 BaseNameCandidates(workspaceRoot, ...)。
func ResolveMentions(
	text string,
	atts []domain.AttachmentDescriptor,
	mode MentionResolveMode,
	workspaceRoot string,
	finder func(basename string) ([]string, error),
) MentionResult {
	mode = NormalizeMentionResolve(mode)
	out := MentionResult{Text: text, Attachments: atts}
	if mode == MentionResolveOff || strings.TrimSpace(text) == "" {
		return out
	}
	if finder == nil {
		finder = func(basename string) ([]string, error) {
			return BaseNameCandidates(workspaceRoot, basename, 8)
		}
	}
	seen := map[string]struct{}{}
	for _, m := range mentionFileToken.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		base := m[1]
		key := strings.ToLower(base)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		hits, err := finder(base)
		if err != nil || len(hits) == 0 {
			continue
		}
		if len(hits) > 1 {
			out.Ambiguous = append(out.Ambiguous, base)
			out.Hints = append(out.Hints, fmt.Sprintf(`[workspace_hint] filename %q is ambiguous (%d hits); use view_image with an explicit path.`, base, len(hits)))
			continue
		}
		alias := hits[0]
		switch mode {
		case MentionResolveHint:
			out.Hints = append(out.Hints, fmt.Sprintf(`[workspace_hint] filename %q uniquely resolves to %q. Use view_image to inspect.`, base, alias))
		case MentionResolveAutoAttach:
			if alreadyAttached(out.Attachments, alias, base) {
				continue
			}
			role := ClassifyMIME("", base)
			id := "mention-" + strings.ToLower(strings.ReplaceAll(base, ".", "-"))
			abs := filepath.Join(workspaceRoot, filepath.FromSlash(alias))
			desc := domain.AttachmentDescriptor{
				ID:             id,
				Name:           base,
				MIME:           guessMIME(base),
				Role:           role,
				Source:         domain.AttachmentSourceWorkspace,
				WorkspaceAlias: alias,
				LocalPath:      abs,
				InputRef: &domain.AttachmentInputRef{
					ID: id, Name: base, Alias: alias, MIME: guessMIME(base), StagedPath: alias,
				},
			}
			out.Attachments = append(out.Attachments, desc)
			out.Hints = append(out.Hints, fmt.Sprintf(`[workspace_hint] auto_attach %q as %q.`, base, alias))
		}
	}
	if len(out.Hints) > 0 {
		out.Text = strings.TrimSpace(text + "\n" + strings.Join(out.Hints, "\n"))
	}
	return out
}

func alreadyAttached(atts []domain.AttachmentDescriptor, alias, name string) bool {
	for _, a := range atts {
		if strings.EqualFold(a.WorkspaceAlias, alias) || strings.EqualFold(a.Name, name) {
			return true
		}
	}
	return false
}

func guessMIME(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".txt", ".md", ".csv":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}
