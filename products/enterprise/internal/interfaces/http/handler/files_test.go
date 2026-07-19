package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"genesis-agent/internal/domain"
	"genesis-agent/products/enterprise/internal/interfaces/http/staging"
)

func TestUploadFileMultipart(t *testing.T) {
	store, err := staging.New(t.TempDir(), staging.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAgentHandlerWithFiles(nil, store)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("hello staging"))
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/files", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rr := httptest.NewRecorder()
	h.UploadFile(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp UploadFileResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID == "" || resp.Role == "" || !strings.Contains(resp.Name, "hello") {
		t.Fatalf("resp=%+v", resp)
	}
}

func TestUploadFileBase64JSONDoc(t *testing.T) {
	store, err := staging.New(t.TempDir(), staging.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAgentHandlerWithFiles(nil, store)
	payload, _ := json.Marshal(UploadFileRequest{
		Name: "note.txt", MIME: "text/plain",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("hello doc")),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/files", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.UploadFile(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestUploadFileRejectsExe(t *testing.T) {
	store, err := staging.New(t.TempDir(), staging.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAgentHandlerWithFiles(nil, store)
	payload, _ := json.Marshal(UploadFileRequest{
		Name: "tool.exe", MIME: "application/octet-stream",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("MZ")),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/files", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.UploadFile(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestResolveStartRunImageBase64(t *testing.T) {
	store, err := staging.New(t.TempDir(), staging.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAgentHandlerWithFiles(nil, store)
	atts, err := h.resolveAttachments([]domain.AttachmentDescriptor{{
		Name: "shot.png", MIME: "image/png",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nfake")),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 1 || atts[0].ContentBase64 != "" || atts[0].ID == "" || atts[0].LocalPath == "" {
		t.Fatalf("atts=%+v", atts[0])
	}
	if atts[0].Role != domain.AttachmentRoleImage {
		t.Fatalf("role=%s", atts[0].Role)
	}
}

func TestResolveStartRunRejectsDocBase64(t *testing.T) {
	store, err := staging.New(t.TempDir(), staging.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAgentHandlerWithFiles(nil, store)
	_, err = h.resolveAttachments([]domain.AttachmentDescriptor{{
		Name: "a.pdf", MIME: "application/pdf",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("%PDF")),
	}})
	if err == nil || !strings.Contains(err.Error(), "仅支持图片") {
		t.Fatalf("err=%v", err)
	}
}
