package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
)

// GateValidator 是 GatePipeline 的可组合校验步骤。
type GateValidator interface {
	Name() string
	Validate(ctx context.Context, state *gateValidationState) error
}

type gateValidationState struct {
	name         string
	declaredMIME string
	size         int64
	content      io.Reader
	buffered     *bufio.Reader
	head         []byte
	ext          string
	detectedMIME string
	kind         string
}

// GatePipeline 组合多个 publication-blocking validator。
type GatePipeline struct {
	version    string
	validators []GateValidator
}

func NewGatePipeline(version string, validators ...GateValidator) GatePipeline {
	if strings.TrimSpace(version) == "" {
		version = "basic-gate/v2"
	}
	return GatePipeline{version: version, validators: validators}
}

func DefaultGatePipeline() GatePipeline {
	return NewGatePipeline("basic-gate/v2",
		SizeLimitGate{},
		MIMEExtensionGate{},
		FormatIntegrityGate{},
	)
}

func (p GatePipeline) Version() string { return p.version }

func (p GatePipeline) Validate(ctx context.Context, name, declaredMIME string, size int64, content io.Reader) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	buffered := bufio.NewReader(content)
	head, err := buffered.Peek(minInt64(size, 512))
	if err != nil && err != io.EOF {
		return "", "", err
	}
	state := &gateValidationState{
		name:         name,
		declaredMIME: declaredMIME,
		size:         size,
		content:      content,
		buffered:     buffered,
		head:         head,
		ext:          strings.ToLower(path.Ext(name)),
		detectedMIME: http.DetectContentType(head),
	}
	for _, validator := range p.validators {
		if err := validator.Validate(ctx, state); err != nil {
			return "", "", wrapGateRejection(validator.Name(), err)
		}
	}
	if state.kind == "" {
		state.kind = strings.TrimPrefix(state.ext, ".")
	}
	if state.detectedMIME == "" {
		state.detectedMIME = state.declaredMIME
	}
	return state.kind, state.detectedMIME, nil
}

func wrapGateRejection(validator string, err error) error {
	if err == nil {
		return nil
	}
	var classified *artifactcontract.Error
	if errors.As(err, &classified) && classified.Code == artifactcontract.ErrCodeArtifactInvalid {
		return err
	}
	return artifactcontract.NewGateError(validator, gateReasonFromError(err), err)
}

func gateReasonFromError(err error) string {
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "为空"):
		return "empty_artifact"
	case strings.Contains(msg, "mime"):
		return "mime_mismatch"
	case strings.Contains(msg, "魔数"), strings.Contains(msg, "utf-8"), strings.Contains(msg, "eof"), strings.Contains(msg, "结构"):
		return "format_integrity"
	case strings.Contains(msg, "越界"), strings.Contains(msg, "压缩比"), strings.Contains(msg, "条目"):
		return "archive_policy"
	default:
		return "gate_rejected"
	}
}

// SizeLimitGate 拒绝空对象。
type SizeLimitGate struct{}

func (SizeLimitGate) Name() string { return "size_limit" }

func (SizeLimitGate) Validate(_ context.Context, state *gateValidationState) error {
	if state.size <= 0 {
		return fmt.Errorf("Artifact 为空")
	}
	return nil
}

// MIMEExtensionGate 校验声明 MIME 与魔数/扩展名的一致性。
type MIMEExtensionGate struct{}

func (MIMEExtensionGate) Name() string { return "mime_extension" }

func (MIMEExtensionGate) Validate(_ context.Context, state *gateValidationState) error {
	if state.declaredMIME != "" && !compatibleMIME(state.declaredMIME, state.detectedMIME, state.ext) {
		return fmt.Errorf("声明 MIME %s 与内容 %s 不一致", state.declaredMIME, state.detectedMIME)
	}
	return nil
}

// FormatIntegrityGate 校验 PDF/Office/ZIP 与文本 UTF-8 的结构完整性。
type FormatIntegrityGate struct{}

func (FormatIntegrityGate) Name() string { return "format_integrity" }

func (FormatIntegrityGate) Validate(ctx context.Context, state *gateValidationState) error {
	switch state.ext {
	case ".pdf":
		if !bytes.HasPrefix(state.head, []byte("%PDF-")) {
			return fmt.Errorf("PDF 魔数无效")
		}
		if err := validatePDFTail(state.content, state.size); err != nil {
			return err
		}
		state.kind, state.detectedMIME = "pdf", "application/pdf"
	case ".pptx", ".docx", ".xlsx", ".zip":
		if !bytes.HasPrefix(state.head, []byte("PK\x03\x04")) {
			return fmt.Errorf("ZIP/Office 容器魔数无效")
		}
		if err := validateArchive(state.content, state.size, state.ext); err != nil {
			return err
		}
		state.kind = strings.TrimPrefix(state.ext, ".")
		state.detectedMIME = firstNonEmpty(mime.TypeByExtension(state.ext), "application/zip")
	case ".md", ".txt", ".json", ".csv", ".yaml", ".yml":
		if !utf8.Valid(state.head) {
			return fmt.Errorf("文本不是有效 UTF-8")
		}
		state.kind = strings.TrimPrefix(state.ext, ".")
		state.detectedMIME = firstNonEmpty(mime.TypeByExtension(state.ext), "text/plain; charset=utf-8")
	default:
		state.kind = strings.TrimPrefix(state.ext, ".")
	}
	return ctx.Err()
}
