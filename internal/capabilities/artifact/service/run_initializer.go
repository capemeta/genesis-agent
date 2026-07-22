package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"path"
	"regexp"
	"strings"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
)

// deliverableExtInPrompt 仅用于推断交付类型（后缀），绝不把自然语言片段当作 DesiredName。
// 用户可见文件名由模型通过产物 ObservedName 声明（对齐 Kode Write path / Codex apply_patch path），
// 或由 API DeclaredDeliverable.DesiredName 显式指定。
var deliverableExtInPrompt = regexp.MustCompile(`(?i)\.(pptx|docx|xlsx|pdf|md|markdown)(?:\b|$)`)

// TaskDeliverableInitializer 将可信任务事实解析为持久化交付契约。
// 优先使用显式 DeclaredDeliverable；仅在无声明且调用方显式 ArtifactRequired 时对 Prompt 做类型启发式。
// 默认路径改为产物证据建约（见 DeterministicFinalizer.ensurePrimaryFromProduced），NLP 不再预建门禁。
type TaskDeliverableInitializer struct {
	store artifactcontract.DeliverableSpecStore
	now   func() time.Time
}

func NewTaskDeliverableInitializer(store artifactcontract.DeliverableSpecStore) (*TaskDeliverableInitializer, error) {
	if store == nil {
		return nil, fmt.Errorf("deliverable initializer 缺少 store")
	}
	return &TaskDeliverableInitializer{store: store, now: time.Now}, nil
}

func (s *TaskDeliverableInitializer) InitializeRun(ctx context.Context, req artifactcontract.RunInitializationRequest) error {
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.RunID) == "" {
		return fmt.Errorf("deliverable initializer 缺少 tenant/run")
	}
	if len(req.Deliverables) > 0 {
		return s.persistDeclared(ctx, req)
	}
	if !req.ArtifactRequired {
		return nil
	}
	return s.persistHeuristic(ctx, req)
}

func (s *TaskDeliverableInitializer) persistDeclared(ctx context.Context, req artifactcontract.RunInitializationRequest) error {
	for i, declared := range req.Deliverables {
		spec, err := declaredToSpec(req.TenantID, req.RunID, i, declared, s.now().UTC())
		if err != nil {
			return err
		}
		if err := s.store.CreateDeliverable(ctx, spec); err != nil && err != artifactcontract.ErrAlreadyExists {
			return err
		}
	}
	return nil
}

func (s *TaskDeliverableInitializer) persistHeuristic(ctx context.Context, req artifactcontract.RunInitializationRequest) error {
	suffix, err := resolveTaskDeliverableType(req.Prompt)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(req.TenantID + "\x00" + req.RunID + "\x00primary"))
	// DesiredName 留空：Publish 时回退到 produced.ObservedName（模型在脚本/命令中写出的文件名即声明）。
	spec := artifactmodel.DeliverableSpec{
		ID: "deliverable-" + hex.EncodeToString(digest[:8]), TenantID: req.TenantID, RunID: req.RunID,
		Required: true, Cardinality: "exactly_one", Role: artifactmodel.DeliverableRolePrimary, DesiredName: "",
		AcceptedMIMEs: []string{mimeForSuffix(suffix)}, AcceptedSuffix: []string{suffix},
		DeliveryPolicy: "run-output", CreatedAt: s.now().UTC(),
	}
	// pptx 默认可挂 soft visual-qa（便于有视觉时记录证据）；不设 QAEnforcement=required，
	// 与「不配置」等价：无视觉模型时不阻塞交付。
	if suffix == ".pptx" {
		spec.QAPolicy = "visual-qa/v1"
	}
	if p := strings.TrimSpace(req.QAPolicy); p != "" {
		spec.QAPolicy = p
	}
	if e := strings.TrimSpace(req.QAEnforcement); e != "" {
		spec.QAEnforcement = e
	}
	if err := s.store.CreateDeliverable(ctx, spec); err != nil && err != artifactcontract.ErrAlreadyExists {
		return err
	}
	return nil
}

func declaredToSpec(tenantID, runID string, index int, declared artifactcontract.DeclaredDeliverable, now time.Time) (artifactmodel.DeliverableSpec, error) {
	role := artifactmodel.DeliverableRole(strings.TrimSpace(declared.Role))
	if role == "" {
		role = artifactmodel.DeliverableRolePrimary
	}
	if role != artifactmodel.DeliverableRolePrimary && role != artifactmodel.DeliverableRoleSupporting {
		return artifactmodel.DeliverableSpec{}, fmt.Errorf("declared deliverable role 无效: %q", declared.Role)
	}
	suffixes := normalizeStringList(declared.AcceptedSuffix)
	mimes := normalizeStringList(declared.AcceptedMIMEs)
	desired := strings.TrimSpace(declared.DesiredName)
	explicitName := desired != ""
	if explicitName {
		desired = path.Base(strings.ReplaceAll(desired, `\`, "/"))
		if desired == "" || desired == "." || desired == ".." || strings.ContainsAny(desired, "/\\\x00") {
			return artifactmodel.DeliverableSpec{}, fmt.Errorf("declared deliverable desired_name 无效")
		}
	}
	if len(suffixes) == 0 && desired != "" {
		if ext := strings.ToLower(path.Ext(desired)); ext != "" {
			suffixes = []string{ext}
		}
	}
	if len(mimes) == 0 && len(suffixes) > 0 {
		if value := mimeForSuffix(suffixes[0]); value != "" {
			mimes = []string{value}
		}
	}
	if len(suffixes) == 0 && len(mimes) == 0 {
		return artifactmodel.DeliverableSpec{}, fmt.Errorf("declared deliverable 须提供 accepted_suffixes、accepted_mimes 或带后缀的 desired_name")
	}
	// 未指定文件名：留空，由模型产物 ObservedName 在 Publish 时声明用户可见名（不从自然语言抠名、不用 stamp 顶替）。
	if !explicitName {
		suffix := ""
		if len(suffixes) > 0 {
			suffix = suffixes[0]
		} else if len(mimes) > 0 {
			suffix = suffixForMIME(mimes[0])
		}
		if suffix == "" {
			return artifactmodel.DeliverableSpec{}, fmt.Errorf("declared deliverable 缺少可派生类型约束的后缀")
		}
		if len(suffixes) == 0 {
			suffixes = []string{suffix}
		}
		desired = ""
	}
	delivery := strings.TrimSpace(declared.DeliveryPolicy)
	if delivery == "" {
		delivery = "run-output"
	}
	id := strings.TrimSpace(declared.ID)
	if id == "" {
		nameKey := desired
		if nameKey == "" {
			nameKey = "produced-name"
			if len(suffixes) > 0 {
				nameKey = "produced-name" + suffixes[0]
			}
		}
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s", tenantID, runID, index, nameKey)))
		id = "deliverable-" + hex.EncodeToString(digest[:8])
	}
	// primary 始终 required；supporting 才使用声明中的 Required（可为可选辅助交付）。
	required := true
	if role == artifactmodel.DeliverableRoleSupporting {
		required = declared.Required
	}
	cardinality := strings.TrimSpace(declared.Cardinality)
	if cardinality == "" {
		if required {
			cardinality = "exactly_one"
		} else {
			cardinality = "zero_or_one"
		}
	}
	spec := artifactmodel.DeliverableSpec{
		ID: id, TenantID: tenantID, RunID: runID, Required: required, Cardinality: cardinality, Role: role,
		DesiredName: desired, AcceptedMIMEs: mimes, AcceptedSuffix: suffixes,
		QAPolicy: strings.TrimSpace(declared.QAPolicy), QAEnforcement: strings.TrimSpace(declared.QAEnforcement),
		DeliveryPolicy: delivery, CreatedAt: now,
	}
	// 未声明 QA 时，pptx 仅挂 soft policy（无 required enforcement）；硬视觉门槛须显式 QAEnforcement=required。
	if spec.QAPolicy == "" && len(suffixes) == 1 && suffixes[0] == ".pptx" && required {
		spec.QAPolicy = "visual-qa/v1"
	}
	if err := spec.Validate(); err != nil {
		return artifactmodel.DeliverableSpec{}, fmt.Errorf("declared deliverable 无效: %w", err)
	}
	return spec, nil
}

func suffixForMIME(mediaType string) string {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	switch {
	case strings.Contains(mediaType, "presentationml"), mediaType == "application/vnd.ms-powerpoint":
		return ".pptx"
	case strings.Contains(mediaType, "wordprocessingml"), mediaType == "application/msword":
		return ".docx"
	case strings.Contains(mediaType, "spreadsheetml"), mediaType == "application/vnd.ms-excel":
		return ".xlsx"
	case mediaType == "application/pdf":
		return ".pdf"
	case mediaType == "text/markdown", mediaType == "text/x-markdown":
		return ".md"
	case mediaType == "text/plain":
		return ".txt"
	case mediaType == "text/csv":
		return ".csv"
	case mediaType == "application/json":
		return ".json"
	default:
		return ""
	}
}

// resolveTaskDeliverableType 只推断交付类型，绝不解析 DesiredName。
func resolveTaskDeliverableType(prompt string) (string, error) {
	lower := strings.ToLower(prompt)
	for _, candidate := range []struct {
		suffix string
		words  []string
	}{
		{".pptx", []string{"pptx", "ppt", "演示文稿", "幻灯片"}},
		{".docx", []string{"docx", "word 文档", "word文件"}},
		{".xlsx", []string{"xlsx", "excel", "电子表格"}},
		{".pdf", []string{"pdf"}},
		{".md", []string{"markdown", "md文档"}},
	} {
		for _, word := range candidate.words {
			if strings.Contains(lower, word) {
				return candidate.suffix, nil
			}
		}
	}
	if m := deliverableExtInPrompt.FindStringSubmatch(prompt); len(m) == 2 {
		ext := "." + strings.ToLower(m[1])
		if ext == ".markdown" {
			ext = ".md"
		}
		return ext, nil
	}
	return "", fmt.Errorf("ARTIFACT_CONTRACT_REQUIRED: artifact_required=true 但无法确定交付文件类型；请通过 Deliverables 显式声明")
}

func mimeForSuffix(suffix string) string {
	if value := mime.TypeByExtension(suffix); value != "" {
		return value
	}
	return map[string]string{".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation", ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".pdf": "application/pdf", ".md": "text/markdown"}[suffix]
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

var _ artifactcontract.RunInitializer = (*TaskDeliverableInitializer)(nil)
