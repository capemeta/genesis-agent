// Package materialize 仅在发往 Provider 的瞬间将 ImageRef 转为可发送字节。
package materialize

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"strings"

	"genesis-agent/internal/capabilities/media/visionio"
	"genesis-agent/internal/domain"

	"golang.org/x/image/draw"
)

const (
	maxBytes            = 10 * 1024 * 1024
	highMaxDimension    = 2048
	highMaxPatches      = 2500 // 32px patches，对齐 Codex high
	originalMaxDimension = 6000
	originalMaxPatches  = 10000 // 对齐 Codex original
	lowMaxDimension     = 512
	patchSize           = 32
	jpegQuality         = 85
)

// ErrMaterialize 表示物化失败，调用方应降级为占位文本。
type ErrMaterialize struct {
	Reason string
}

func (e ErrMaterialize) Error() string {
	return e.Reason
}

// PlaceholderFor 对齐 Codex 风格的占位文案。
func PlaceholderFor(err error, ref *domain.ImageRef) string {
	label := "image"
	if ref != nil {
		if a := strings.TrimSpace(ref.PathAlias); a != "" {
			label = a
		} else if a := strings.TrimSpace(ref.CandidateID); a != "" {
			label = a
		}
	}
	msg := "image content omitted because it could not be processed"
	if err != nil {
		s := err.Error()
		switch {
		case strings.Contains(s, "image_too_large"), strings.Contains(s, "exceeded"):
			msg = "image content omitted because it exceeded the supported size limit; use a smaller image"
		case strings.Contains(s, "missing local read path"), strings.Contains(s, "missing bytes"):
			msg = "image content omitted because the image source is unavailable"
		}
	}
	return fmt.Sprintf("[%s: %s]", label, msg)
}

// ToDataURL 读取 LocalReadPath / InlineBytes（含 ephemeral 登记），按 detail 缩放后返回 RFC2397 data URL。
func ToDataURL(ref *domain.ImageRef) (string, error) {
	return ToDataURLContext(context.Background(), ref)
}

// ToDataURLContext 带并发配额的物化（出站读图路径）。
func ToDataURLContext(ctx context.Context, ref *domain.ImageRef) (string, error) {
	if err := visionio.Acquire(ctx); err != nil {
		return "", err
	}
	defer visionio.Release()
	ApplyEphemeral(ref)
	raw, mime, err := loadRaw(ref)
	if err != nil {
		return "", err
	}
	detail := "auto"
	if ref != nil && strings.TrimSpace(ref.Detail) != "" {
		detail = strings.ToLower(strings.TrimSpace(ref.Detail))
	}
	out, outMIME, err := resizeForDetail(raw, mime, detail)
	if err != nil {
		return "", ErrMaterialize{Reason: err.Error()}
	}
	return "data:" + outMIME + ";base64," + base64.StdEncoding.EncodeToString(out), nil
}

// loadRaw 优先 InlineBytes，其次 LocalReadPath。
func loadRaw(ref *domain.ImageRef) ([]byte, string, error) {
	if ref == nil {
		return nil, "", ErrMaterialize{Reason: "nil image ref"}
	}
	mime := strings.TrimSpace(ref.MediaType)
	if mime == "" {
		mime = "image/png"
	}
	if len(ref.InlineBytes) > 0 {
		if int64(len(ref.InlineBytes)) > maxBytes {
			return nil, "", ErrMaterialize{Reason: fmt.Sprintf("image_too_large: %d", len(ref.InlineBytes))}
		}
		return ref.InlineBytes, mime, nil
	}
	path := strings.TrimSpace(ref.LocalReadPath)
	if path == "" {
		return nil, "", ErrMaterialize{Reason: "image_ref missing local read path; cannot materialize"}
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", ErrMaterialize{Reason: fmt.Sprintf("open image: %v", err)}
	}
	if info.Size() > maxBytes {
		return nil, "", ErrMaterialize{Reason: fmt.Sprintf("image_too_large: %d", info.Size())}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", ErrMaterialize{Reason: err.Error()}
	}
	if int64(len(raw)) > maxBytes {
		return nil, "", ErrMaterialize{Reason: fmt.Sprintf("image_too_large: %d", len(raw))}
	}
	return raw, mime, nil
}

func resizeForDetail(raw []byte, mime, detail string) ([]byte, string, error) {
	img, format, err := decodeImage(raw)
	if err != nil {
		return nil, "", fmt.Errorf("decode: %w", err)
	}
	maxDim, maxPatches := limitsForDetail(detail)
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := fitDimensions(w, h, maxDim, maxPatches)
	if nw == w && nh == h {
		// 无需缩放：尽量原样返回（已在大小限制内）
		if format == "jpeg" || format == "jpg" {
			return raw, "image/jpeg", nil
		}
		if format == "png" {
			return raw, "image/png", nil
		}
		if format == "gif" {
			return raw, "image/gif", nil
		}
		// webp 等统一重编码为 png
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/png", nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	var buf bytes.Buffer
	outMIME := "image/png"
	switch format {
	case "jpeg", "jpg":
		outMIME = "image/jpeg"
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return nil, "", err
		}
	default:
		if err := png.Encode(&buf, dst); err != nil {
			return nil, "", err
		}
	}
	if buf.Len() > maxBytes {
		return nil, "", fmt.Errorf("image_too_large after resize: %d", buf.Len())
	}
	return buf.Bytes(), outMIME, nil
}

func limitsForDetail(detail string) (maxDim, maxPatches int) {
	switch strings.ToLower(strings.TrimSpace(detail)) {
	case "low":
		return lowMaxDimension, 512
	case "original":
		return originalMaxDimension, originalMaxPatches
	case "high", "auto", "":
		return highMaxDimension, highMaxPatches
	default:
		return highMaxDimension, highMaxPatches
	}
}

func fitDimensions(w, h, maxDim, maxPatches int) (int, int) {
	if w <= 0 || h <= 0 {
		return w, h
	}
	scale := 1.0
	if w > maxDim || h > maxDim {
		sw := float64(maxDim) / float64(w)
		sh := float64(maxDim) / float64(h)
		scale = math.Min(sw, sh)
	}
	nw := int(math.Max(1, math.Round(float64(w)*scale)))
	nh := int(math.Max(1, math.Round(float64(h)*scale)))
	patches := patchCount(nw, nh)
	if maxPatches > 0 && patches > maxPatches {
		factor := math.Sqrt(float64(maxPatches) / float64(patches))
		nw = int(math.Max(1, math.Round(float64(nw)*factor)))
		nh = int(math.Max(1, math.Round(float64(nh)*factor)))
	}
	return nw, nh
}

func patchCount(w, h int) int {
	pw := (w + patchSize - 1) / patchSize
	ph := (h + patchSize - 1) / patchSize
	return pw * ph
}

func decodeImage(raw []byte) (image.Image, string, error) {
	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", err
	}
	return img, format, nil
}

func init() {
	// 注册标准解码器（jpeg/png/gif 由 image 包自动注册）
	_ = png.Encode
	_ = jpeg.Encode
	_ = gif.Decode
}
