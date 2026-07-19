// Package attach 将本地路径转为 TurnInput AttachmentDescriptor（不含 bytes）。
package attach

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"genesis-agent/internal/capabilities/turninput"
	"genesis-agent/internal/domain"
)

// FromPaths 读取本地文件元数据并分类；LocalPath 仅供本进程瞬态使用。
func FromPaths(paths []string) ([]domain.AttachmentDescriptor, error) {
	out := make([]domain.AttachmentDescriptor, 0, len(paths))
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		abs, err := filepath.Abs(raw)
		if err != nil {
			return nil, fmt.Errorf("attach %q: %w", raw, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("attach %q: %w", raw, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("attach %q: is a directory", raw)
		}
		sum, err := hashFile(abs)
		if err != nil {
			return nil, err
		}
		mimeType := detectMIME(abs)
		name := filepath.Base(abs)
		role := turninput.ClassifyMIME(mimeType, name)
		id := "att-" + sum[:16]
		desc := domain.AttachmentDescriptor{
			ID:             id,
			Name:           name,
			MIME:           mimeType,
			SHA256:         sum,
			Size:           info.Size(),
			Role:           role,
			Source:         domain.AttachmentSourceCLIAttach,
			WorkspaceAlias: filepath.ToSlash(raw),
			LocalPath:      abs,
			InputRef: &domain.AttachmentInputRef{
				ID:         id,
				Name:       name,
				Alias:      filepath.ToSlash(raw),
				SHA256:     sum,
				MIME:       mimeType,
				StagedPath: filepath.ToSlash(raw),
				Size:       info.Size(),
			},
		}
		if role == domain.AttachmentRoleDocument {
			if excerpt, ok := tryPlainExcerpt(abs, mimeType); ok {
				desc.ExtractedText = excerpt
			}
		}
		out = append(out, desc)
	}
	return out, nil
}

func tryPlainExcerpt(path, mimeType string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".csv", ".json", ".yaml", ".yml":
	default:
		if !strings.HasPrefix(mimeType, "text/") {
			return "", false
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	if len(raw) > turninput.MaxExtractedTextBytes {
		raw = raw[:turninput.MaxExtractedTextBytes]
	}
	return string(raw), true
}

// ParseAtMentions 从用户文本提取 @path token，返回净化文本与路径列表。
func ParseAtMentions(text string) (clean string, paths []string) {
	fields := strings.Fields(text)
	kept := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.HasPrefix(f, "@") && len(f) > 1 {
			p := strings.TrimPrefix(f, "@")
			p = strings.Trim(p, `"'`)
			if p != "" {
				paths = append(paths, p)
				continue
			}
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, " "), paths
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func detectMIME(path string) string {
	if ext := filepath.Ext(path); ext != "" {
		if m := mime.TypeByExtension(ext); m != "" {
			return m
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(buf[:n])
}
