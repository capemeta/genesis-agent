package contract

import (
	"context"
	"strings"
)

type completionPolicyContextKey struct{}
type qaRecorderContextKey struct{}
type evidenceQAHintsContextKey struct{}

// EvidenceQAHints 是 Skill fork 等可信控制面注入的 QA 偏好，供「产物证据建约」使用。
// 不单独构成交付承诺；仅在 FinalizeRequired 因产物证据创建 Spec 时覆盖默认 soft QA。
type EvidenceQAHints struct {
	Policy      string
	Enforcement string
}

func WithCompletionPolicy(ctx context.Context, policy CompletionPolicy) context.Context {
	if policy == nil {
		return ctx
	}
	return context.WithValue(ctx, completionPolicyContextKey{}, policy)
}

func WithQAEvidenceRecorder(ctx context.Context, recorder QAEvidenceRecorder) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, qaRecorderContextKey{}, recorder)
}

// WithEvidenceQAHints 注入产物证据建约时的 QA 偏好（可空字段表示沿用默认）。
func WithEvidenceQAHints(ctx context.Context, hints EvidenceQAHints) context.Context {
	if ctx == nil {
		return ctx
	}
	hints.Policy = strings.TrimSpace(hints.Policy)
	hints.Enforcement = strings.TrimSpace(hints.Enforcement)
	if hints.Policy == "" && hints.Enforcement == "" {
		return ctx
	}
	return context.WithValue(ctx, evidenceQAHintsContextKey{}, hints)
}

func EvidenceQAHintsFromContext(ctx context.Context) (EvidenceQAHints, bool) {
	if ctx == nil {
		return EvidenceQAHints{}, false
	}
	value, ok := ctx.Value(evidenceQAHintsContextKey{}).(EvidenceQAHints)
	return value, ok && (value.Policy != "" || value.Enforcement != "")
}

func QAEvidenceRecorderFromContext(ctx context.Context) (QAEvidenceRecorder, bool) {
	if ctx == nil {
		return nil, false
	}
	value, ok := ctx.Value(qaRecorderContextKey{}).(QAEvidenceRecorder)
	return value, ok && value != nil
}

func CompletionPolicyFromContext(ctx context.Context) (CompletionPolicy, bool) {
	if ctx == nil {
		return nil, false
	}
	value, ok := ctx.Value(completionPolicyContextKey{}).(CompletionPolicy)
	return value, ok && value != nil
}
