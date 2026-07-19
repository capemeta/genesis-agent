package turninput

import (
	"os"
	"path/filepath"
	"strings"

	"genesis-agent/internal/capabilities/turninput/extract"
	"genesis-agent/internal/domain"
)

// PrepareAttachments 按 document_extract 模式填充或清空 ExtractedText。
// path_only/off：强制清空预抽正文；preview：对缺正文的本地文档尝试 Extractor。
func PrepareAttachments(atts []domain.AttachmentDescriptor, mode DocumentExtractMode, reg *extract.Registry) []domain.AttachmentDescriptor {
	if len(atts) == 0 {
		return atts
	}
	mode = NormalizeDocumentExtract(mode)
	out := make([]domain.AttachmentDescriptor, len(atts))
	copy(out, atts)
	if !ShouldExtractDocuments(mode) {
		for i := range out {
			out[i].ExtractedText = ""
		}
		return out
	}
	if reg == nil {
		reg = extract.DefaultRegistry()
	}
	for i := range out {
		att := &out[i]
		role := att.Role
		if role == "" {
			role = ClassifyMIME(att.MIME, att.Name)
			att.Role = role
		}
		if role != domain.AttachmentRoleDocument {
			continue
		}
		if strings.TrimSpace(att.ExtractedText) != "" {
			if len(att.ExtractedText) > MaxExtractedTextBytes {
				att.ExtractedText = att.ExtractedText[:MaxExtractedTextBytes]
			}
			continue
		}
		path := strings.TrimSpace(att.LocalPath)
		if path == "" {
			continue
		}
		text, err := reg.Extract(path, att.MIME, MaxExtractedTextBytes)
		if err != nil || strings.TrimSpace(text) == "" {
			continue // 降级：仅保留 path 提示
		}
		att.ExtractedText = text
	}
	return out
}

// BaseNameCandidates 在工作区按 basename 唯一性查找（mention_resolve 用）。
func BaseNameCandidates(workspaceRoot, basename string, maxHits int) ([]string, error) {
	basename = filepath.Base(strings.TrimSpace(basename))
	if basename == "" || basename == "." || basename == ".." {
		return nil, nil
	}
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		root = "."
	}
	if maxHits <= 0 {
		maxHits = 8
	}
	var hits []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == ".gocache" || name == ".gomodcache" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(info.Name(), basename) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		hits = append(hits, filepath.ToSlash(rel))
		if len(hits) >= maxHits {
			return errStopWalk
		}
		return nil
	})
	if err != nil && err != errStopWalk {
		return hits, err
	}
	return hits, nil
}

type stopWalk struct{}

func (stopWalk) Error() string { return "stop" }

var errStopWalk error = stopWalk{}
