// Package view_image 实现看图取图原语（不在工具内调用 LLM）。
package view_image

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	"genesis-agent/internal/capabilities/llm/vision"
	"genesis-agent/internal/capabilities/media/materialize"
	"genesis-agent/internal/capabilities/media/visionio"
	tool "genesis-agent/internal/capabilities/tool/contract"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/domain"
)

const (
	errNotAnImage        = "not_an_image"
	errVisionUnavailable = "vision_unavailable"
	// msgVisionUnavailable 形态 C：明确禁止 Pillow 等伪看图弯路。
	msgVisionUnavailable = "vision_unavailable: this run has no image-capable model (set models.*.supports_image=true and/or router.vision). " +
		"Do NOT use sandbox_exec/run_command with Pillow/OpenCV/ImageMagick/numpy pixel stats to invent image content. " +
		"Tell the user honestly that visual understanding is unavailable. " +
		"Allowed: filename/size/MIME metadata only if the user explicitly asked for file info; text extract for documents is OK."
	errInvalidPath                   = "invalid_image_path"
	errImageTooLarge                 = "image_too_large"
	errProducedResourceExpired       = "PRODUCED_RESOURCE_EXPIRED"
	maxImageBytes              int64 = 10 * 1024 * 1024
)

// ModeFromContext 从 ctx 读取 EffectiveVisionMode（由 Runtime 注入）。
type modeKey struct{}

func WithVisionMode(ctx context.Context, mode vision.Mode) context.Context {
	return context.WithValue(ctx, modeKey{}, mode)
}

func VisionModeFromContext(ctx context.Context) (vision.Mode, bool) {
	mode, ok := ctx.Value(modeKey{}).(vision.Mode)
	return mode, ok
}

// Tool 看图原语。
type Tool struct {
	deps      toolkit.Deps
	produced  workcontract.ProducedResourceStore // 可选：校验 leased candidate
	readers   workcontract.ResourceReaderRouter  // 可选：candidate 物化读字节
	manifests workcontract.RunManifestStore      // 可选：跨 Run 读子产物所属 binding 的 backend
}

type input struct {
	CandidateID string `json:"candidate_id,omitempty"`
	Path        string `json:"path,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

type output struct {
	OK              bool             `json:"ok"`
	Error           string           `json:"error,omitempty"`
	Message         string           `json:"message,omitempty"`
	SuggestedAction string           `json:"suggested_action,omitempty"`
	ImageRef        *domain.ImageRef `json:"image_ref,omitempty"`
	VisionMode      string           `json:"vision_mode,omitempty"`
	InjectImage     bool             `json:"inject_image,omitempty"` // Runtime 据此决定是否写入 Parts
	RerenderHint    string           `json:"rerender_hint,omitempty"`
}

// New 创建 view_image。
func New(deps toolkit.Deps) (tool.Tool, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}
	return &Tool{deps: deps}, nil
}

// WithProducedStore 注入 ProducedResourceStore 以校验 leased candidate 过期。
func WithProducedStore(t tool.Tool, store workcontract.ProducedResourceStore) tool.Tool {
	vt, ok := t.(*Tool)
	if !ok || vt == nil {
		return t
	}
	vt.produced = store
	return vt
}

// WithReaders 注入 ResourceReaderRouter，供 candidate_id 物化像素。
func WithReaders(t tool.Tool, readers workcontract.ResourceReaderRouter) tool.Tool {
	vt, ok := t.(*Tool)
	if !ok || vt == nil {
		return t
	}
	vt.readers = readers
	return vt
}

// WithManifests 注入 RunManifestStore，供跨 Run 读子产物时按其所属 binding 恢复 backend。
func WithManifests(t tool.Tool, manifests workcontract.RunManifestStore) tool.Tool {
	vt, ok := t.(*Tool)
	if !ok || vt == nil {
		return t
	}
	vt.manifests = manifests
	return vt
}

func (t *Tool) GetInfo() *tool.Info {
	return tool.WithTraits(&tool.Info{
		Name: "view_image",
		Description: "将 workspace 相对路径、宿主机可读绝对路径或 produced candidate_id 指向的图片加载为可视觉消费的 ImageRef。" +
			"不要对 docx/pdf 等非图片使用。" +
			"若返回 vision_unavailable：禁止改用 Pillow/像素分析伪看图，须如实告知用户无视觉能力。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"candidate_id": {Type: "string", Description: "leased ProducedResource / candidate id"},
				"path":         {Type: "string", Description: "workspace-relative image path"},
				"detail":       {Type: "string", Description: "low | high | auto | original（original≈Codex 高保真预算）", Enum: []string{"low", "high", "auto", "original"}},
			},
		},
	}, tool.ToolTraits{ReadOnly: true, ConcurrencySafe: true, Exposure: tool.ToolExposureDirect})
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolkit.DecodeParams(params, &in); err != nil {
		return "", err
	}
	mode, _ := VisionModeFromContext(ctx)
	if mode == "" {
		mode = vision.ModeDegradedText
	}
	if mode == vision.ModeDegradedText {
		return toolkit.ToJSON(output{OK: false, Error: errVisionUnavailable, Message: msgVisionUnavailable, VisionMode: string(mode)})
	}
	if err := visionio.Acquire(ctx); err != nil {
		return "", err
	}
	defer visionio.Release()

	candidate := strings.TrimSpace(in.CandidateID)
	rawPath := strings.TrimSpace(in.Path)
	if candidate == "" && rawPath == "" {
		return toolkit.ToJSON(output{OK: false, Error: errInvalidPath, Message: "candidate_id or path required", VisionMode: string(mode)})
	}
	if err := rejectAbsoluteOrRemotePhysical(rawPath); err != nil {
		return toolkit.ToJSON(output{OK: false, Error: errInvalidPath, Message: err.Error(), VisionMode: string(mode)})
	}
	if candidate != "" && rawPath == "" {
		return t.executeCandidate(ctx, candidate, in.Detail, mode)
	}

	pathRef, err := toolkit.ResolveRequire(ctx, t.deps, "view_image", rawPath, permission.OperationRead, fscontract.ResolveOptions{
		Operation: string(permission.OperationRead),
		MustExist: true,
	})
	if err != nil {
		return "", err
	}
	stat, err := t.deps.Backend.Stat(ctx, pathRef)
	if err != nil {
		return "", err
	}
	if stat.Size > maxImageBytes {
		return toolkit.ToJSON(output{OK: false, Error: errImageTooLarge, Message: fmt.Sprintf("image exceeds %d bytes", maxImageBytes), VisionMode: string(mode)})
	}
	// 经 Backend Reader 读全量字节（避免 Materializer 旁路 os.ReadFile）
	raw, readErr := t.deps.Backend.Read(ctx, pathRef, fscontract.ReadOptions{MaxBytes: maxImageBytes + 1})
	if readErr != nil && fscontract.CodeOf(readErr) != fscontract.ErrCodeTooLarge {
		return "", readErr
	}
	if int64(len(raw)) > maxImageBytes {
		return toolkit.ToJSON(output{OK: false, Error: errImageTooLarge, Message: fmt.Sprintf("image exceeds %d bytes", maxImageBytes), VisionMode: string(mode)})
	}
	mime := sniffImageMIME(pathRef.DisplayPath, raw)
	if mime == "" {
		return toolkit.ToJSON(output{OK: false, Error: errNotAnImage, Message: "file is not a supported image; use document channel", VisionMode: string(mode)})
	}
	ref := &domain.ImageRef{
		PathAlias:   pathRef.DisplayPath,
		MediaType:   mime,
		SHA256:      toolkit.HashBytes(raw),
		Detail:      normalizeDetail(in.Detail),
		InlineBytes: raw,
	}
	materialize.RememberRef(ref)
	wire := *ref
	wire.InlineBytes = nil
	return toolkit.ToJSON(output{
		OK: true, ImageRef: &wire, VisionMode: string(mode),
		InjectImage: mode == vision.ModeDirectInject,
		Message:     "image_ref ready",
	})
}

func (t *Tool) executeCandidate(ctx context.Context, candidate, detail string, mode vision.Mode) (string, error) {
	if expired, hint := t.checkCandidateExpired(ctx, candidate); expired {
		return toolkit.ToJSON(output{
			OK: false, Error: errProducedResourceExpired, VisionMode: string(mode),
			SuggestedAction: "rerun_thumbnail_and_view_image",
			Message:         "leased produced resource expired; re-render QA thumbnails then view_image with new candidate_id",
			RerenderHint:    hint,
		})
	}
	ref := &domain.ImageRef{
		CandidateID:        candidate,
		ProducedResourceID: candidate,
		Detail:             normalizeDetail(detail),
	}
	if err := t.hydrateCandidate(ctx, ref); err != nil {
		return toolkit.ToJSON(output{
			OK: false, Error: errInvalidPath, VisionMode: string(mode),
			Message: err.Error(),
		})
	}
	materialize.RememberRef(ref)
	// JSON 不携带 InlineBytes；ephemeral Remember 供 outbound 使用
	wire := *ref
	wire.InlineBytes = nil
	wire.LocalReadPath = ""
	return toolkit.ToJSON(output{
		OK: true, ImageRef: &wire, VisionMode: string(mode),
		InjectImage: mode == vision.ModeDirectInject,
		Message:     "image_ref from candidate_id; bytes registered for outbound materialize",
	})
}

func (t *Tool) hydrateCandidate(ctx context.Context, ref *domain.ImageRef) error {
	if t == nil || t.produced == nil || t.readers == nil {
		return fmt.Errorf("candidate_id requires ProducedResourceStore and ResourceReaderRouter")
	}
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		return fmt.Errorf("candidate_id requires prepared run context")
	}
	// 先在当前 Run scope 解析；未命中则按父在 finish 时写下的 AdoptionRecord 定位所属子 Run（跨 Run 读，spec §4.1 Case B）。
	ownerTenant := prepared.Manifest.Scope.TenantID
	ownerRun := prepared.Manifest.RunID
	desc, err := t.produced.Get(ctx, ownerTenant, ownerRun, ref.CandidateID)
	if err != nil {
		if adoptions, configured := artifactcontract.AdoptionStoreFromContext(ctx); configured {
			if rec, ok := adoptions.Resolve(ownerTenant, ownerRun, ref.CandidateID); ok && rec.OwnerRunID != "" {
				ownerTenant, ownerRun = rec.OwnerTenantID, rec.OwnerRunID
				desc, err = t.produced.Get(ctx, ownerTenant, ownerRun, ref.CandidateID)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("produced resource not found: %w", err)
	}
	// 按资源所属 binding 的 backend 读取，而非父 manifest 硬匹配（spec §7.2）。
	execution, err := t.resolveOwnerExecution(ctx, prepared.Manifest, ownerTenant, ownerRun, desc.BindingID)
	if err != nil {
		return err
	}
	handle, err := t.readers.Open(ctx, execution.Backend, desc.Source)
	if err != nil {
		return fmt.Errorf("open produced resource: %w", err)
	}
	defer handle.Reader.Close()
	if handle.Size > maxImageBytes {
		return fmt.Errorf("image exceeds %d bytes", maxImageBytes)
	}
	raw, err := io.ReadAll(io.LimitReader(handle.Reader, maxImageBytes+1))
	if err != nil {
		return fmt.Errorf("read produced resource: %w", err)
	}
	if int64(len(raw)) > maxImageBytes {
		return fmt.Errorf("image exceeds %d bytes", maxImageBytes)
	}
	mime := desc.MediaType
	if mime == "" {
		mime = handle.MediaType
	}
	if mime == "" {
		mime = sniffImageMIME(desc.ObservedName, raw)
	}
	if mime == "" {
		return fmt.Errorf("%s", errNotAnImage)
	}
	ref.MediaType = mime
	ref.PathAlias = desc.ObservedName
	ref.InlineBytes = raw
	ref.SHA256 = toolkit.HashBytes(raw)
	return nil
}

// resolveOwnerExecution 按资源所属 binding 恢复其执行快照（含 backend），禁止父 manifest 硬匹配。
// 同 Run（含 sandbox_exec 经 AddExecution 追加的 remote binding）直接用当前 manifest；
// 跨 Run（子产物）读所属 Run 的 manifest 定位 binding backend。
func (t *Tool) resolveOwnerExecution(ctx context.Context, parentManifest workmodel.RunManifest, ownerTenant, ownerRun, bindingID string) (workmodel.PreparedExecutionSnapshot, error) {
	if ownerRun == "" || ownerRun == parentManifest.RunID {
		if exec, ok := findExecution(parentManifest.Executions, bindingID); ok {
			return exec, nil
		}
	}
	if t.manifests != nil && ownerRun != "" {
		if manifest, err := t.manifests.Get(ctx, ownerTenant, ownerRun); err == nil {
			if exec, ok := findExecution(manifest.Executions, bindingID); ok {
				return exec, nil
			}
		}
	}
	// 兜底：再查当前 manifest（覆盖 owner 解析回退到本 Run 的情况）。
	if exec, ok := findExecution(parentManifest.Executions, bindingID); ok {
		return exec, nil
	}
	return workmodel.PreparedExecutionSnapshot{}, fmt.Errorf("produced resource binding not resolvable (owner_run=%s)", ownerRun)
}

func findExecution(executions []workmodel.PreparedExecutionSnapshot, bindingID string) (workmodel.PreparedExecutionSnapshot, bool) {
	for _, candidate := range executions {
		if candidate.Binding.ID == bindingID {
			return candidate, true
		}
	}
	return workmodel.PreparedExecutionSnapshot{}, false
}

func rejectAbsoluteOrRemotePhysical(p string) error {
	if p == "" {
		return nil
	}
	cleaned := filepath.Clean(p)
	if strings.HasPrefix(cleaned, "/workspace/") {
		return fmt.Errorf("remote physical path forbidden")
	}
	return nil
}

func normalizeDetail(d string) string {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "low", "high", "auto", "original":
		return strings.ToLower(strings.TrimSpace(d))
	default:
		return "auto"
	}
}

func sniffImageMIME(name string, head []byte) string {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	}
	if len(head) >= 3 && head[0] == 0xff && head[1] == 0xd8 && head[2] == 0xff {
		return "image/jpeg"
	}
	if len(head) >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n" {
		return "image/png"
	}
	if len(head) >= 6 && (string(head[:6]) == "GIF87a" || string(head[:6]) == "GIF89a") {
		return "image/gif"
	}
	return ""
}

// ParseOutput 供 Runtime 解析 tool JSON。
func ParseOutput(content string) (output, error) {
	var out output
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return output{}, err
	}
	return out, nil
}

func (t *Tool) checkCandidateExpired(ctx context.Context, candidateID string) (bool, string) {
	if t == nil || t.produced == nil {
		return false, ""
	}
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		return false, ""
	}
	desc, err := t.produced.Get(ctx, prepared.Manifest.Scope.TenantID, prepared.Manifest.RunID, candidateID)
	if err != nil {
		return false, ""
	}
	if desc.Availability != workmodel.ResourceAvailabilityLeased {
		return false, ""
	}
	if desc.ExpiresAt == nil || desc.ExpiresAt.After(time.Now()) {
		return false, ""
	}
	hint := "run_skill_command with thumbnail.py / pdftoppm to regenerate QA images; Harness will register a new leased candidate_id; then call view_image(candidate_id=...)"
	return true, hint
}
