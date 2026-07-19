package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTextContentPrefersParts(t *testing.T) {
	t.Parallel()
	m := NewUserMessageWithParts("fallback", []ContentPart{
		{Type: ContentPartText, Text: "hello "},
		{Type: ContentPartImage, ImageRef: &ImageRef{PathAlias: "a.png", MediaType: "image/png"}},
		{Type: ContentPartText, Text: "world"},
	})
	if got := m.TextContent(); got != "hello world" {
		t.Fatalf("TextContent=%q", got)
	}
	if !m.HasImageParts() {
		t.Fatal("expected image parts")
	}
}

func TestImageRefJSONHasNoBytesField(t *testing.T) {
	t.Parallel()
	m := NewUserMessageWithParts("看图", []ContentPart{
		{Type: ContentPartText, Text: "看图"},
		{Type: ContentPartImage, ImageRef: &ImageRef{PathAlias: "x.png", MediaType: "image/png", SHA256: "abc"}},
	})
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, bad := range []string{"image_bytes", "base64", "ImageBytes"} {
		if strings.Contains(s, bad) {
			t.Fatalf("persisted json must not contain %q: %s", bad, s)
		}
	}
	if !strings.Contains(s, "image_ref") || !strings.Contains(s, "x.png") {
		t.Fatalf("expected image_ref in json: %s", s)
	}
}
