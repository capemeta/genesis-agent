package contract

import "context"

type completionPolicyContextKey struct{}
type qaRecorderContextKey struct{}

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
