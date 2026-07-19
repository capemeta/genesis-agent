package staging

import "testing"

func TestIsImageMIME(t *testing.T) {
	if !IsImageMIME("image/png", "a.png") {
		t.Fatal("png")
	}
	if IsImageMIME("application/pdf", "a.pdf") {
		t.Fatal("pdf not image for StartRun base64")
	}
}

func TestIsAllowedUpload(t *testing.T) {
	cases := []struct {
		mime, name string
		want       bool
	}{
		{"image/png", "a.png", true},
		{"application/pdf", "a.pdf", true},
		{"", "report.docx", true},
		{"", "clip.mp4", true},
		{"", "song.mp3", true},
		{"", "pack.zip", true},
		{"application/octet-stream", "malware.exe", false},
		{"", "tool.bin", false},
		{"", "lib.dll", false},
	}
	for _, tc := range cases {
		if got := IsAllowedUpload(tc.mime, tc.name); got != tc.want {
			t.Fatalf("%s/%s got %v want %v", tc.mime, tc.name, got, tc.want)
		}
	}
}
