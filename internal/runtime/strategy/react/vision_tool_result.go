package react

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactservice "genesis-agent/internal/capabilities/artifact/service"
	"genesis-agent/internal/capabilities/llm/vision"
	viewimage "genesis-agent/internal/capabilities/media/tool/view_image"
	"genesis-agent/internal/capabilities/turninput"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime"
)

func visionToolResultParts(rc *runtime.RunContext, content string) []domain.ContentPart {
	if rc == nil || rc.VisionMode != string(vision.ModeDirectInject) {
		return nil
	}
	out, err := viewimage.ParseOutput(content)
	if err != nil || !out.OK || out.ImageRef == nil || !out.InjectImage {
		return nil
	}
	meta := fmt.Sprintf("view_image ok path=%s media=%s", out.ImageRef.PathAlias, out.ImageRef.MediaType)
	return []domain.ContentPart{
		{Type: domain.ContentPartText, Text: meta},
		{Type: domain.ContentPartImage, ImageRef: out.ImageRef},
	}
}

// applyViewImageRuntimeBridge 在工具结果入 Messages 前做形态分流与过期引导。
func (e *ReactLoopEngine) applyViewImageRuntimeBridge(ctx context.Context, rc *runtime.RunContext, content string) (string, []domain.ContentPart) {
	content = annotateExpiredLeaseGuidance(content, rc)
	if out, err := viewimage.ParseOutput(content); err == nil && (!out.OK && out.Error == "vision_unavailable") {
		_ = tryRecordVisionUnavailable(ctx, nil, true)
		content = annotateVisionUnavailable(content)
	}
	parts := visionToolResultParts(rc, content)
	if rc != nil && rc.VisionMode == string(vision.ModeExpertRoute) {
		content = e.rewriteViewImageForExpertRoute(ctx, rc, content)
		parts = nil
	}
	return content, parts
}

// annotateVisionUnavailable 形态 C：在工具结果上追加禁止伪看图的 harness 引导。
func annotateVisionUnavailable(content string) string {
	if !strings.Contains(content, "vision_unavailable") {
		return content
	}
	if strings.Contains(content, "[harness_bridge]") {
		return content
	}
	return content + "\n[harness_bridge] vision_unavailable. suggested_action=honest_degrade_or_configure_vision. " +
		"Forbidden: Pillow/OpenCV/ImageMagick/numpy pixel-stat pseudo-vision via sandbox_exec or run_command. " +
		"Required: tell the user visual understanding is unavailable; suggest configuring models.*.supports_image and/or router.vision. " +
		"Allowed: filename/size/MIME only if user asked for file metadata; document text extract for non-image files."
}

func (e *ReactLoopEngine) rewriteViewImageForExpertRoute(ctx context.Context, rc *runtime.RunContext, content string) string {
	out, err := viewimage.ParseOutput(content)
	if err != nil {
		return content
	}
	if !out.OK {
		return annotateExpiredLeaseGuidance(content, rc)
	}
	if out.ImageRef == nil {
		return content
	}
	if e == nil || e.visionExpert == nil {
		payload, _ := json.Marshal(map[string]any{
			"ok": false, "mode": string(vision.ModeExpertRoute),
			"passed": false, "defects": []string{"vision_expert_not_configured"},
		})
		return string(payload)
	}
	res, err := e.visionExpert.Analyze(ctx, *out.ImageRef, "")
	if err != nil {
		payload, _ := json.Marshal(map[string]any{
			"ok": false, "mode": string(vision.ModeExpertRoute),
			"passed": false, "defects": []string{err.Error()},
		})
		return string(payload)
	}
	_ = tryRecordVisualQAFromText(ctx, res.Text)
	// 包装便于主模型消费：明确这是视觉专家结论，不是 vision_unavailable。
	return "[vision_expert]\n" + res.Text
}

// enrichUserTurnWithVisionExpert 形态 B：首轮用户图不进主会话 Parts，先经 Expert 并入文本。
func (e *ReactLoopEngine) enrichUserTurnWithVisionExpert(ctx context.Context, msg *domain.Message, attachments []domain.AttachmentDescriptor) *domain.Message {
	if e == nil || e.visionExpert == nil || msg == nil || len(attachments) == 0 {
		return msg
	}
	var sb strings.Builder
	analyzed := 0
	for i := range attachments {
		att := attachments[i]
		role := att.Role
		if role == "" {
			role = turninput.ClassifyMIME(att.MIME, att.Name)
		}
		if role != domain.AttachmentRoleImage {
			continue
		}
		ref := domain.ImageRef{
			PathAlias:     firstNonEmpty(att.WorkspaceAlias, att.Name),
			MediaType:     att.MIME,
			SHA256:        att.SHA256,
			Width:         att.Width,
			Height:        att.Height,
			AttachmentID:  att.ID,
			LocalReadPath: att.LocalPath,
		}
		res, err := e.visionExpert.Analyze(ctx, ref, "")
		sb.WriteString("\n[vision_expert:")
		sb.WriteString(ref.PathAlias)
		sb.WriteString("]\n")
		if err != nil {
			sb.WriteString(`{"passed":false,"defects":["`)
			sb.WriteString(strings.ReplaceAll(err.Error(), `"`, `'`))
			sb.WriteString(`"]}`)
		} else {
			sb.WriteString(res.Text)
			_ = tryRecordVisualQAFromText(ctx, res.Text)
		}
		analyzed++
	}
	if analyzed == 0 {
		return msg
	}
	body := msg.TextContent() + sb.String()
	return domain.NewUserMessageWithParts(body, []domain.ContentPart{{Type: domain.ContentPartText, Text: body}})
}

func annotateExpiredLeaseGuidance(content string, rc *runtime.RunContext) string {
	if !strings.Contains(content, "PRODUCED_RESOURCE_EXPIRED") {
		return content
	}
	if strings.Contains(content, "[harness_bridge]") {
		return content
	}
	out, err := viewimage.ParseOutput(content)
	hint := ""
	if err == nil {
		hint = strings.TrimSpace(out.RerenderHint)
	}
	if hint == "" {
		hint = "Re-run thumbnail.py / pdftoppm via run_skill_command, wait for new leased candidate_id, then view_image again."
	}
	if rc != nil && rc.SkillFollow != nil {
		pending := rc.SkillFollow.PendingQACommands()
		if len(pending) == 0 {
			pending = rc.SkillFollow.QACommands()
		}
		var renderCmds []string
		for _, cmd := range pending {
			low := strings.ToLower(cmd)
			if strings.Contains(low, "thumbnail") || strings.Contains(low, "pdftoppm") {
				renderCmds = append(renderCmds, cmd)
			}
		}
		if len(renderCmds) > 0 {
			hint = "Preferred re-render via run_skill_command: " + strings.Join(renderCmds, "; ") + ". Then view_image with the new leased candidate_id."
		}
	}
	return content + "\n[harness_bridge] leased QA image expired. suggested_action=rerun_thumbnail_and_view_image. " + hint
}

func tryRecordVisualQAFromText(ctx context.Context, text string) error {
	recorder, ok := artifactcontract.QAEvidenceRecorderFromContext(ctx)
	if !ok {
		return nil
	}
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		return nil
	}
	signOff, hit := artifactservice.ParseVisualChecklist(text)
	if !hit {
		signOff, hit = artifactservice.ParseExpertVisualJSON(text)
	}
	if !hit || !signOff.Passed {
		return nil
	}
	return recorder.RecordPassed(ctx, artifactcontract.QAPassRequest{
		TenantID:  prepared.Manifest.Scope.TenantID,
		RunID:     prepared.Manifest.RunID,
		PolicyID:  artifactservice.ValidatorVisualQA,
		Validator: artifactservice.ValidatorVisualQA,
	})
}

// tryRecordVisionUnavailable 形态 C：写入 degraded 证据，禁止伪 passed。
// force=true 用于 view_image 返回 vision_unavailable；force=false 时仅在存在 image 附件时写入。
func tryRecordVisionUnavailable(ctx context.Context, attachments []domain.AttachmentDescriptor, force bool) error {
	if !force {
		hasImage := false
		for _, att := range attachments {
			role := att.Role
			if role == "" {
				role = turninput.ClassifyMIME(att.MIME, att.Name)
			}
			if role == domain.AttachmentRoleImage {
				hasImage = true
				break
			}
		}
		if !hasImage {
			return nil
		}
	}
	recorder, ok := artifactcontract.QAEvidenceRecorderFromContext(ctx)
	if !ok {
		return nil
	}
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		return nil
	}
	return recorder.RecordDegraded(ctx, artifactcontract.QADegradeRequest{
		TenantID:    prepared.Manifest.Scope.TenantID,
		RunID:       prepared.Manifest.RunID,
		PolicyID:    artifactservice.ValidatorVisualQA,
		Validator:   artifactservice.ValidatorVisualQA,
		FailureCode: "vision_unavailable",
		Status:      "degraded",
	})
}
