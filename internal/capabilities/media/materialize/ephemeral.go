package materialize

import (
	"sync"

	"genesis-agent/internal/domain"
)

type ephemeralSource struct {
	LocalReadPath string
	InlineBytes   []byte
	MediaType     string
}

var ephemeral sync.Map // key -> ephemeralSource

// Remember 在工具执行进程内登记可物化源（JSON 往返会丢掉 LocalReadPath/InlineBytes）。
func Remember(key string, localPath string, inline []byte, mime string) {
	key = normalizeKey(key)
	if key == "" {
		return
	}
	var copied []byte
	if len(inline) > 0 {
		copied = append([]byte(nil), inline...)
	}
	ephemeral.Store(key, ephemeralSource{LocalReadPath: localPath, InlineBytes: copied, MediaType: mime})
}

// RememberRef 根据 ImageRef 生成 key 并登记。
func RememberRef(ref *domain.ImageRef) {
	if ref == nil {
		return
	}
	Remember(RefKey(ref), ref.LocalReadPath, ref.InlineBytes, ref.MediaType)
}

// RefKey 稳定 ephemeral key。
func RefKey(ref *domain.ImageRef) string {
	if ref == nil {
		return ""
	}
	if id := normalizeKey(ref.CandidateID); id != "" {
		return "c:" + id
	}
	if id := normalizeKey(ref.ProducedResourceID); id != "" {
		return "p:" + id
	}
	alias := normalizeKey(ref.PathAlias)
	if alias == "" {
		return ""
	}
	return "a:" + alias + ":" + normalizeKey(ref.SHA256)
}

// ApplyEphemeral 把进程内源写回 ref（不持久化）。
func ApplyEphemeral(ref *domain.ImageRef) {
	if ref == nil {
		return
	}
	key := RefKey(ref)
	if key == "" {
		return
	}
	v, ok := ephemeral.Load(key)
	if !ok {
		return
	}
	src := v.(ephemeralSource)
	if ref.LocalReadPath == "" && src.LocalReadPath != "" {
		ref.LocalReadPath = src.LocalReadPath
	}
	if len(ref.InlineBytes) == 0 && len(src.InlineBytes) > 0 {
		ref.InlineBytes = append([]byte(nil), src.InlineBytes...)
	}
	if ref.MediaType == "" && src.MediaType != "" {
		ref.MediaType = src.MediaType
	}
}

func normalizeKey(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}
