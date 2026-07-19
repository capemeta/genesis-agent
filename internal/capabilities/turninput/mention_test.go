package turninput

import (
	"path/filepath"
	"strings"
	"testing"

	"genesis-agent/internal/domain"
)

func TestPrepareAttachmentsPathOnlyClearsText(t *testing.T) {
	atts := []domain.AttachmentDescriptor{{
		ID: "1", Name: "a.txt", Role: domain.AttachmentRoleDocument, ExtractedText: "hello",
	}}
	got := PrepareAttachments(atts, DocumentExtractPathOnly, nil)
	if got[0].ExtractedText != "" {
		t.Fatalf("expected empty, got %q", got[0].ExtractedText)
	}
}

func TestMentionResolveHintUnique(t *testing.T) {
	finder := func(basename string) ([]string, error) {
		if basename == "111.png" {
			return []string{"assets/111.png"}, nil
		}
		return nil, nil
	}
	res := ResolveMentions("描述下 111.png 的内容", nil, MentionResolveHint, ".", finder)
	if len(res.Hints) != 1 || !strings.Contains(res.Text, "assets/111.png") {
		t.Fatalf("res=%+v", res)
	}
	if len(res.Attachments) != 0 {
		t.Fatal("hint must not attach")
	}
}

func TestMentionResolveAutoAttach(t *testing.T) {
	finder := func(string) ([]string, error) { return []string{"img/a.png"}, nil }
	res := ResolveMentions("看 a.png", nil, MentionResolveAutoAttach, "C:\\ws", finder)
	if len(res.Attachments) != 1 || res.Attachments[0].Role != domain.AttachmentRoleImage {
		t.Fatalf("atts=%+v", res.Attachments)
	}
	if res.Attachments[0].WorkspaceAlias != "img/a.png" {
		t.Fatalf("alias=%s", res.Attachments[0].WorkspaceAlias)
	}
	wantLocal := filepath.Join("C:\\ws", filepath.FromSlash("img/a.png"))
	if res.Attachments[0].LocalPath != wantLocal {
		t.Fatalf("local=%s want=%s", res.Attachments[0].LocalPath, wantLocal)
	}
}

func TestMentionResolveAmbiguous(t *testing.T) {
	finder := func(string) ([]string, error) { return []string{"a/x.png", "b/x.png"}, nil }
	res := ResolveMentions("x.png", nil, MentionResolveHint, ".", finder)
	if len(res.Ambiguous) != 1 {
		t.Fatalf("ambiguous=%v", res.Ambiguous)
	}
}

func TestDefaultOptionsForProduct(t *testing.T) {
	if DefaultOptionsForProduct("enterprise").DocumentExtract != DocumentExtractPreview {
		t.Fatal("enterprise should preview")
	}
	if DefaultOptionsForProduct("cli").DocumentExtract != DocumentExtractPathOnly {
		t.Fatal("cli should path_only")
	}
}
