package sanitize

import (
	"strings"
	"testing"

	"genesis-agent/internal/domain"
)

func TestStripImages(t *testing.T) {
	t.Parallel()
	msgs := []*domain.Message{
		domain.NewUserMessageWithParts("hi", []domain.ContentPart{
			{Type: domain.ContentPartText, Text: "hi"},
			{Type: domain.ContentPartImage, ImageRef: &domain.ImageRef{PathAlias: "a.png"}},
		}),
	}
	kept := StripImages(msgs, true, "main")
	if !kept[0].HasImageParts() {
		t.Fatal("should keep images when supported")
	}
	stripped := StripImages(msgs, false, "main")
	if stripped[0].HasImageParts() {
		t.Fatal("should strip images")
	}
	if !strings.Contains(stripped[0].TextContent(), "does not support image input") {
		t.Fatalf("placeholder missing: %q", stripped[0].TextContent())
	}
}

func TestCompactHistoricalImages(t *testing.T) {
	t.Parallel()
	mk := func(name string) *domain.Message {
		return domain.NewUserMessageWithParts(name, []domain.ContentPart{
			{Type: domain.ContentPartText, Text: name},
			{Type: domain.ContentPartImage, ImageRef: &domain.ImageRef{PathAlias: name}},
		})
	}
	msgs := []*domain.Message{mk("old.png"), mk("mid.png"), mk("new.png")}
	out := CompactHistoricalImages(msgs, 2)
	if out[0].HasImageParts() {
		t.Fatal("oldest should be compacted")
	}
	if !strings.Contains(out[0].TextContent(), "historical image ref") {
		t.Fatalf("got %q", out[0].TextContent())
	}
	if !out[1].HasImageParts() || !out[2].HasImageParts() {
		t.Fatal("recent two should keep images")
	}
}
