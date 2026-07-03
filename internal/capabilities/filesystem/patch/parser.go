// Package patch 提供 Codex 风格 apply_patch 的解析与应用。
package patch

import (
	"fmt"
	"strings"
)

const (
	beginMarker   = "*** Begin Patch"
	endMarker     = "*** End Patch"
	addMarker     = "*** Add File: "
	deleteMarker  = "*** Delete File: "
	updateMarker  = "*** Update File: "
	moveMarker    = "*** Move to: "
	contextMarker = "@@"
	contextPrefix = "@@ "
	eofMarker     = "*** End of File"
)

// HunkType 描述 patch hunk 类型。
type HunkType string

const (
	HunkAdd    HunkType = "add"
	HunkDelete HunkType = "delete"
	HunkUpdate HunkType = "update"
)

// Hunk 是一个文件变更。
type Hunk struct {
	Type     HunkType
	Path     string
	MovePath string
	Content  string
	Chunks   []Chunk
}

// Chunk 是 Update File 中的一段变更。
type Chunk struct {
	ChangeContext string
	OldLines      []string
	NewLines      []string
	EndOfFile     bool
}

// ParseError 是稳定的 patch 解析错误。
type ParseError struct {
	Line    int
	Message string
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("invalid patch hunk on line %d: %s", e.Line, e.Message)
	}
	return "invalid patch: " + e.Message
}

// Parse 解析 Codex apply_patch 格式。兼容 <<EOF heredoc 包裹。
func Parse(text string) ([]Hunk, error) {
	lines := patchLines(text)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != beginMarker {
		return nil, &ParseError{Message: "The first line of the patch must be '*** Begin Patch'"}
	}
	if strings.TrimSpace(lines[len(lines)-1]) != endMarker {
		return nil, &ParseError{Message: "The last line of the patch must be '*** End Patch'"}
	}
	var hunks []Hunk
	for i := 1; i < len(lines)-1; {
		trimmed := strings.TrimSpace(lines[i])
		switch {
		case trimmed == "":
			i++
		case strings.HasPrefix(trimmed, "*** Environment ID:"):
			id := strings.TrimSpace(strings.TrimPrefix(trimmed, "*** Environment ID:"))
			if id == "" {
				return nil, &ParseError{Message: "apply_patch environment_id cannot be empty"}
			}
			i++
		case strings.HasPrefix(trimmed, addMarker):
			h, next, err := parseAdd(lines, i)
			if err != nil {
				return nil, err
			}
			hunks = append(hunks, h)
			i = next
		case strings.HasPrefix(trimmed, deleteMarker):
			hunks = append(hunks, Hunk{Type: HunkDelete, Path: strings.TrimPrefix(trimmed, deleteMarker)})
			i++
		case strings.HasPrefix(trimmed, updateMarker):
			h, next, err := parseUpdate(lines, i)
			if err != nil {
				return nil, err
			}
			hunks = append(hunks, h)
			i = next
		default:
			return nil, &ParseError{Line: i + 1, Message: fmt.Sprintf("'%s' is not a valid hunk header. Valid hunk headers: '*** Add File: {path}', '*** Delete File: {path}', '*** Update File: {path}'", trimmed)}
		}
	}
	if len(hunks) == 0 {
		return nil, fmt.Errorf("No files were modified.")
	}
	return hunks, nil
}

func patchLines(text string) []string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) >= 4 {
		first := strings.TrimSpace(lines[0])
		last := strings.TrimSpace(lines[len(lines)-1])
		if (first == "<<EOF" || first == "<<'EOF'" || first == "<<\"EOF\"") && strings.HasSuffix(last, "EOF") {
			lines = lines[1 : len(lines)-1]
		}
	}
	return lines
}

func isHeader(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == endMarker || strings.HasPrefix(trimmed, addMarker) || strings.HasPrefix(trimmed, deleteMarker) || strings.HasPrefix(trimmed, updateMarker)
}

func parseAdd(lines []string, start int) (Hunk, int, error) {
	path := strings.TrimPrefix(strings.TrimSpace(lines[start]), addMarker)
	var b strings.Builder
	for i := start + 1; i < len(lines); i++ {
		if isHeader(lines[i]) {
			if b.Len() == 0 {
				return Hunk{}, 0, &ParseError{Line: start + 1, Message: fmt.Sprintf("Add file hunk for path '%s' is empty", path)}
			}
			return Hunk{Type: HunkAdd, Path: path, Content: b.String()}, i, nil
		}
		if !strings.HasPrefix(lines[i], "+") {
			return Hunk{}, 0, &ParseError{Line: i + 1, Message: fmt.Sprintf("'%s' is not a valid hunk header. Valid hunk headers: '*** Add File: {path}', '*** Delete File: {path}', '*** Update File: {path}'", strings.TrimSpace(lines[i]))}
		}
		b.WriteString(strings.TrimPrefix(lines[i], "+"))
		b.WriteByte('\n')
	}
	return Hunk{}, 0, &ParseError{Message: "The last line of the patch must be '*** End Patch'"}
}

func parseUpdate(lines []string, start int) (Hunk, int, error) {
	h := Hunk{Type: HunkUpdate, Path: strings.TrimPrefix(strings.TrimSpace(lines[start]), updateMarker)}
	for i := start + 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\t ")
		if isHeader(line) {
			if len(h.Chunks) == 0 {
				return Hunk{}, 0, &ParseError{Line: start + 1, Message: fmt.Sprintf("Update file hunk for path '%s' is empty", h.Path)}
			}
			if lastChunkEmpty(h.Chunks) {
				return Hunk{}, 0, &ParseError{Line: i + 1, Message: "Update hunk does not contain any lines"}
			}
			return h, i, nil
		}
		if len(h.Chunks) == 0 && h.MovePath == "" && strings.HasPrefix(line, moveMarker) {
			h.MovePath = strings.TrimPrefix(line, moveMarker)
			continue
		}
		if line == contextMarker || strings.HasPrefix(line, contextPrefix) {
			if lastChunkEmpty(h.Chunks) {
				return Hunk{}, 0, &ParseError{Line: i + 1, Message: fmt.Sprintf("Unexpected line found in update hunk: '%s'. Every line should start with ' ' (context line), '+' (added line), or '-' (removed line)", line)}
			}
			ctx := ""
			if strings.HasPrefix(line, contextPrefix) {
				ctx = strings.TrimPrefix(line, contextPrefix)
			}
			h.Chunks = append(h.Chunks, Chunk{ChangeContext: ctx})
			continue
		}
		if line == eofMarker {
			if lastChunkEmpty(h.Chunks) {
				return Hunk{}, 0, &ParseError{Line: i + 1, Message: "Update hunk does not contain any lines"}
			}
			h.Chunks[len(h.Chunks)-1].EndOfFile = true
			continue
		}
		if len(h.Chunks) == 0 {
			h.Chunks = append(h.Chunks, Chunk{})
		}
		chunk := &h.Chunks[len(h.Chunks)-1]
		switch {
		case lines[i] == "":
			chunk.OldLines = append(chunk.OldLines, "")
			chunk.NewLines = append(chunk.NewLines, "")
		case strings.HasPrefix(lines[i], " "):
			v := strings.TrimPrefix(lines[i], " ")
			chunk.OldLines = append(chunk.OldLines, v)
			chunk.NewLines = append(chunk.NewLines, v)
		case strings.HasPrefix(lines[i], "+"):
			chunk.NewLines = append(chunk.NewLines, strings.TrimPrefix(lines[i], "+"))
		case strings.HasPrefix(lines[i], "-"):
			chunk.OldLines = append(chunk.OldLines, strings.TrimPrefix(lines[i], "-"))
		default:
			return Hunk{}, 0, &ParseError{Line: i + 1, Message: fmt.Sprintf("Expected update hunk to start with a @@ context marker, got: '%s'", lines[i])}
		}
	}
	return Hunk{}, 0, &ParseError{Message: "The last line of the patch must be '*** End Patch'"}
}

func lastChunkEmpty(chunks []Chunk) bool {
	if len(chunks) == 0 {
		return false
	}
	last := chunks[len(chunks)-1]
	return len(last.OldLines) == 0 && len(last.NewLines) == 0
}
