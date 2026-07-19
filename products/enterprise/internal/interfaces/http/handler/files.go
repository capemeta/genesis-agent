package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"genesis-agent/internal/domain"
	"genesis-agent/products/enterprise/internal/interfaces/http/staging"
)

// UploadFileRequest 可选 JSON 上传体（与 multipart 等价，便于脚本；类型须在上传白名单内）。
type UploadFileRequest struct {
	Name          string `json:"name"`
	MIME          string `json:"mime,omitempty"`
	ContentBase64 string `json:"content_base64,omitempty"`
}

// UploadFileResponse 只返回标识与元数据。
type UploadFileResponse struct {
	ID             string                     `json:"id"`
	Name           string                     `json:"name"`
	MIME           string                     `json:"mime"`
	SHA256         string                     `json:"sha256"`
	Size           int64                      `json:"size"`
	Role           domain.AttachmentRole      `json:"role"`
	WorkspaceAlias string                     `json:"workspace_alias,omitempty"`
	InputRef       *domain.AttachmentInputRef `json:"input_ref,omitempty"`
}

// UploadFile POST /v1/files — multipart 主路径；JSON+content_base64 可选。
// 接受常用文档/图片/音视频/压缩包；拒绝任意二进制（见 staging.IsAllowedUpload）。
func (h *AgentHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.files == nil {
		writeError(w, http.StatusServiceUnavailable, "file staging 未配置")
		return
	}
	ct := r.Header.Get("Content-Type")
	var desc domain.AttachmentDescriptor
	var err error
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		if err := r.ParseMultipartForm(h.filesMax()); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("multipart 解析失败: %v", err))
			return
		}
		file, hdr, openErr := r.FormFile("file")
		if openErr != nil {
			writeError(w, http.StatusBadRequest, "缺少 file 字段")
			return
		}
		defer file.Close()
		name := hdr.Filename
		mimeType := hdr.Header.Get("Content-Type")
		desc, err = h.files.Put(name, mimeType, file)
	case strings.HasPrefix(ct, "application/json"):
		var req UploadFileRequest
		if decErr := json.NewDecoder(io.LimitReader(r.Body, h.filesMax()+4096)).Decode(&req); decErr != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("JSON 解析失败: %v", decErr))
			return
		}
		if strings.TrimSpace(req.ContentBase64) == "" || strings.TrimSpace(req.Name) == "" {
			writeError(w, http.StatusBadRequest, "name 与 content_base64 必填")
			return
		}
		raw, decErr := base64.StdEncoding.DecodeString(req.ContentBase64)
		if decErr != nil {
			writeError(w, http.StatusBadRequest, "content_base64 无效")
			return
		}
		mimeType := req.MIME
		if mimeType == "" {
			mimeType = staging.DetectContentType(req.Name, raw)
		}
		if !staging.IsAllowedUpload(mimeType, req.Name) {
			writeError(w, http.StatusBadRequest, "attachment_mime_denied")
			return
		}
		desc, err = h.files.Put(req.Name, mimeType, bytes.NewReader(raw))
	default:
		writeError(w, http.StatusUnsupportedMediaType, "仅支持 multipart/form-data 或 application/json")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "attachment_too_large") {
			writeError(w, http.StatusRequestEntityTooLarge, "attachment_too_large")
			return
		}
		if strings.Contains(err.Error(), "attachment_mime_denied") {
			writeError(w, http.StatusBadRequest, "attachment_mime_denied")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, UploadFileResponse{
		ID: desc.ID, Name: desc.Name, MIME: desc.MIME, SHA256: desc.SHA256, Size: desc.Size,
		Role: desc.Role, WorkspaceAlias: desc.WorkspaceAlias, InputRef: desc.InputRef,
	})
}

func (h *AgentHandler) filesMax() int64 {
	return staging.DefaultMaxBytes
}
