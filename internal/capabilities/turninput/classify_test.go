package turninput

import (
	"strings"
	"testing"

	"genesis-agent/internal/capabilities/llm/vision"
	"genesis-agent/internal/domain"
)

func TestClassifyMIME(t *testing.T) {
	t.Parallel()
	if ClassifyMIME("image/png", "a.png") != domain.AttachmentRoleImage {
		t.Fatal("png")
	}
	if ClassifyMIME("application/vnd.openxmlformats-officedocument.wordprocessingml.document", "文档2.docx") != domain.AttachmentRoleDocument {
		t.Fatal("docx")
	}
	if ClassifyMIME("application/zip", "x.zip") != domain.AttachmentRoleOther {
		t.Fatal("zip")
	}
}

func TestBuildUserTurnMessageDocNeverImagePart(t *testing.T) {
	t.Parallel()
	atts := []domain.AttachmentDescriptor{
		{ID: "1", Name: "111.png", MIME: "image/png", Role: domain.AttachmentRoleImage, WorkspaceAlias: "111.png"},
		{ID: "2", Name: "文档2.docx", MIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Role: domain.AttachmentRoleDocument, ExtractedText: "文档222222", WorkspaceAlias: "uploads/文档2.docx"},
	}
	msg := BuildUserTurnMessage("分析下图片和文件", atts, vision.ModeDirectInject)
	if !msg.HasImageParts() {
		t.Fatal("expected image part")
	}
	for _, p := range msg.Parts {
		if p.Type == domain.ContentPartImage && p.ImageRef != nil && p.ImageRef.PathAlias == "uploads/文档2.docx" {
			t.Fatal("docx must not be image part")
		}
	}
	if !strings.Contains(msg.TextContent(), "文档222222") {
		t.Fatalf("expected extracted text: %q", msg.TextContent())
	}

	expert := BuildUserTurnMessage("分析", atts, vision.ModeExpertRoute)
	if expert.HasImageParts() {
		t.Fatal("expert_route must not inject image parts into main message")
	}

	deg := BuildUserTurnMessage("分析", atts, vision.ModeDegradedText)
	if deg.HasImageParts() {
		t.Fatal("degraded must not inject image parts")
	}
	if !strings.Contains(deg.TextContent(), "Pillow") {
		t.Fatalf("degraded note should forbid Pillow: %q", deg.TextContent())
	}
}
