// Package staging 提供 Enterprise 用户上传附件的进程内 staging（开发期；生产可换对象存储）。
package staging

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"genesis-agent/internal/capabilities/turninput"
	"genesis-agent/internal/domain"
)

const (
	DefaultMaxBytes = 32 << 20 // 32MB
)

// File 是已 staging 的附件元数据。
type File struct {
	ID        string
	Name      string
	MIME      string
	SHA256    string
	Size      int64
	Role      domain.AttachmentRole
	LocalPath string
	CreatedAt time.Time
}

// Store 租户级上传 staging。
type Store struct {
	root     string
	maxBytes int64
	mu       sync.RWMutex
	byID     map[string]File
}

// New 创建 staging store；root 为空则用系统 Temp。
func New(root string, maxBytes int64) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		root = filepath.Join(os.TempDir(), "genesis-enterprise-uploads")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir staging: %w", err)
	}
	return &Store{root: root, maxBytes: maxBytes, byID: map[string]File{}}, nil
}

// Put 写入字节并返回 Descriptor（含 LocalPath，仅服务端瞬态）。
func (s *Store) Put(name string, mimeType string, r io.Reader) (domain.AttachmentDescriptor, error) {
	if s == nil {
		return domain.AttachmentDescriptor{}, fmt.Errorf("staging store nil")
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" {
		name = "upload.bin"
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(name))
	}
	if !IsAllowedUpload(mimeType, name) {
		return domain.AttachmentDescriptor{}, fmt.Errorf("attachment_mime_denied")
	}
	limited := io.LimitReader(r, s.maxBytes+1)
	tmp, err := os.CreateTemp(s.root, "up-*.bin")
	if err != nil {
		return domain.AttachmentDescriptor{}, err
	}
	tmpPath := tmp.Name()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), limited)
	closeErr := tmp.Close()
	if err != nil {
		_ = os.Remove(tmpPath)
		return domain.AttachmentDescriptor{}, err
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return domain.AttachmentDescriptor{}, closeErr
	}
	if n > s.maxBytes {
		_ = os.Remove(tmpPath)
		return domain.AttachmentDescriptor{}, fmt.Errorf("attachment_too_large")
	}
	sum := hex.EncodeToString(h.Sum(nil))
	id := "file-" + sum[:16]
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	final := filepath.Join(s.root, id+"_"+sanitizeName(name))
	if err := os.Rename(tmpPath, final); err != nil {
		_ = os.Remove(tmpPath)
		return domain.AttachmentDescriptor{}, err
	}
	role := turninput.ClassifyMIME(mimeType, name)
	desc := domain.AttachmentDescriptor{
		ID:             id,
		Name:           name,
		MIME:           mimeType,
		SHA256:         sum,
		Size:           n,
		Role:           role,
		Source:         domain.AttachmentSourceUpload,
		WorkspaceAlias: "uploads/" + name,
		LocalPath:      final,
		InputRef: &domain.AttachmentInputRef{
			ID: id, Name: name, Alias: "uploads/" + name, SHA256: sum, MIME: mimeType, StagedPath: final, Size: n,
		},
	}
	s.mu.Lock()
	s.byID[id] = File{
		ID: id, Name: name, MIME: mimeType, SHA256: sum, Size: n, Role: role, LocalPath: final, CreatedAt: time.Now().UTC(),
	}
	s.mu.Unlock()
	return desc, nil
}

// Get 按 id 取 staging 记录。
func (s *Store) Get(id string) (File, bool) {
	if s == nil {
		return File{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.byID[strings.TrimSpace(id)]
	return f, ok
}

// ResolveAttachments 处理 StartRun 附件：
// 1) 图片 content_base64 → staging 后清空 base64；
// 2) 仅含 id → 补齐 LocalPath / 元数据。
func (s *Store) ResolveAttachments(atts []domain.AttachmentDescriptor) ([]domain.AttachmentDescriptor, error) {
	if s == nil || len(atts) == 0 {
		return atts, nil
	}
	out := make([]domain.AttachmentDescriptor, len(atts))
	copy(out, atts)
	for i := range out {
		att := &out[i]
		if b64 := strings.TrimSpace(att.ContentBase64); b64 != "" {
			if err := s.ingestImageBase64(att); err != nil {
				return nil, err
			}
			continue
		}
		if strings.TrimSpace(att.LocalPath) != "" {
			continue
		}
		id := strings.TrimSpace(att.ID)
		if id == "" && att.InputRef != nil {
			id = strings.TrimSpace(att.InputRef.ID)
		}
		if id == "" {
			continue
		}
		f, ok := s.Get(id)
		if !ok {
			continue
		}
		att.ID = f.ID
		if att.Name == "" {
			att.Name = f.Name
		}
		if att.MIME == "" {
			att.MIME = f.MIME
		}
		if att.SHA256 == "" {
			att.SHA256 = f.SHA256
		}
		if att.Size == 0 {
			att.Size = f.Size
		}
		if att.Role == "" {
			att.Role = f.Role
		}
		if att.Source == "" {
			att.Source = domain.AttachmentSourceUpload
		}
		att.LocalPath = f.LocalPath
		att.ContentBase64 = ""
		if att.WorkspaceAlias == "" {
			att.WorkspaceAlias = "uploads/" + f.Name
		}
		if att.InputRef == nil {
			att.InputRef = &domain.AttachmentInputRef{
				ID: f.ID, Name: f.Name, Alias: att.WorkspaceAlias, SHA256: f.SHA256, MIME: f.MIME, StagedPath: f.LocalPath, Size: f.Size,
			}
		}
	}
	return out, nil
}

func (s *Store) ingestImageBase64(att *domain.AttachmentDescriptor) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(att.ContentBase64))
	if err != nil {
		return fmt.Errorf("content_base64 无效: %w", err)
	}
	name := strings.TrimSpace(att.Name)
	if name == "" {
		name = "image.png"
	}
	mimeType := strings.TrimSpace(att.MIME)
	if mimeType == "" {
		mimeType = DetectContentType(name, raw)
	}
	if !IsImageMIME(mimeType, name) {
		return fmt.Errorf("StartRun content_base64 仅支持图片（jpeg/png/webp/gif）；其它类型请先 POST /files")
	}
	staged, err := s.Put(name, mimeType, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	att.ID = staged.ID
	att.Name = staged.Name
	att.MIME = staged.MIME
	att.SHA256 = staged.SHA256
	att.Size = staged.Size
	att.Role = domain.AttachmentRoleImage
	if att.Source == "" {
		att.Source = domain.AttachmentSourceUpload
	}
	att.WorkspaceAlias = staged.WorkspaceAlias
	att.LocalPath = staged.LocalPath
	att.InputRef = staged.InputRef
	att.ContentBase64 = "" // 禁止继续向下传递 / 落盘
	return nil
}

// DetectContentType 嗅探 MIME。
func DetectContentType(name string, head []byte) string {
	if ext := filepath.Ext(name); ext != "" {
		if m := mime.TypeByExtension(ext); m != "" {
			return m
		}
	}
	if len(head) > 0 {
		return http.DetectContentType(head)
	}
	return "application/octet-stream"
}

func sanitizeName(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, name)
	if name == "" {
		return "file"
	}
	return name
}
