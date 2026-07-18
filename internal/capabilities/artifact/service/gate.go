package service

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
)

// BasicGate 提供文本非空/UTF-8 与常见二进制魔数的基础门禁。
// 默认由 SizeLimit + MIME/ext + 格式完整性组成的 GatePipeline 实现。
type BasicGate struct {
	pipeline GatePipeline
}

func (g BasicGate) pipelineOrDefault() GatePipeline {
	if len(g.pipeline.validators) == 0 {
		return DefaultGatePipeline()
	}
	return g.pipeline
}

func (BasicGate) Version() string { return DefaultGatePipeline().Version() }

func (g BasicGate) Validate(ctx context.Context, name, declaredMIME string, size int64, content io.Reader) (string, string, error) {
	return g.pipelineOrDefault().Validate(ctx, name, declaredMIME, size, content)
}

func validatePDFTail(content io.Reader, size int64) error {
	reader, ok := content.(io.ReaderAt)
	if !ok {
		return fmt.Errorf("PDF 门禁需要可随机读取的 quarantine 对象")
	}
	tailSize := minInt64(size, 2048)
	tail := make([]byte, tailSize)
	if _, err := reader.ReadAt(tail, size-int64(tailSize)); err != nil && err != io.EOF {
		return fmt.Errorf("读取 PDF 尾部失败: %w", err)
	}
	if !bytes.Contains(tail, []byte("%%EOF")) {
		return fmt.Errorf("PDF 缺少 EOF 结构标记")
	}
	return nil
}

func validateArchive(content io.Reader, size int64, ext string) error {
	reader, ok := content.(io.ReaderAt)
	if !ok {
		return fmt.Errorf("ZIP/Office 门禁需要可随机读取的 quarantine 对象")
	}
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return fmt.Errorf("ZIP/Office 结构无效: %w", err)
	}
	if len(archive.File) == 0 || len(archive.File) > maxArchiveEntries {
		return fmt.Errorf("ZIP/Office 条目数不合规: %d", len(archive.File))
	}
	required := officeRequiredEntry(ext)
	foundRequired := required == ""
	var expanded int64
	for _, entry := range archive.File {
		name := strings.ReplaceAll(entry.Name, `\`, "/")
		if name == required {
			foundRequired = true
		}
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") || strings.Contains(name, "\x00") {
			return fmt.Errorf("ZIP/Office 含越界条目 %q", entry.Name)
		}
		expanded += int64(entry.UncompressedSize64)
		if expanded > maxArchiveExpandedSize {
			return fmt.Errorf("ZIP/Office 展开后总量超过限制")
		}
		if entry.UncompressedSize64 > 0 && (entry.CompressedSize64 == 0 || entry.UncompressedSize64/entry.CompressedSize64 > maxCompressionRatio) {
			return fmt.Errorf("ZIP/Office 条目 %q 压缩比超过限制", entry.Name)
		}
	}
	if !foundRequired {
		return fmt.Errorf("%s 缺少必要结构 %s", strings.TrimPrefix(ext, "."), required)
	}
	return nil
}

const (
	maxArchiveEntries      = 10_000
	maxArchiveExpandedSize = int64(512 * 1024 * 1024)
	maxCompressionRatio    = uint64(100)
)

func officeRequiredEntry(ext string) string {
	switch ext {
	case ".pptx":
		return "ppt/presentation.xml"
	case ".docx":
		return "word/document.xml"
	case ".xlsx":
		return "xl/workbook.xml"
	default:
		return ""
	}
}

func compatibleMIME(declared, detected, ext string) bool {
	declared = strings.ToLower(strings.TrimSpace(strings.Split(declared, ";")[0]))
	detected = strings.ToLower(strings.TrimSpace(strings.Split(detected, ";")[0]))
	if declared == detected || declared == "application/octet-stream" {
		return true
	}
	if (ext == ".pptx" || ext == ".docx" || ext == ".xlsx" || ext == ".zip") && detected == "application/zip" {
		return true
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minInt64(left int64, right int) int {
	if left < int64(right) {
		return int(left)
	}
	return right
}
