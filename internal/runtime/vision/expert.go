// Package vision 提供 Runtime 侧 VisionExpert（形态 B）；工具不得直接调用本包以外的裸 LLM。
package vision

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainvision "genesis-agent/internal/capabilities/llm/vision"
	llm "genesis-agent/internal/capabilities/llm/contract"
	trace "genesis-agent/internal/capabilities/trace/contract"
	"genesis-agent/internal/domain"
)

// Analyzer 分析图片并返回给主会话的纯文本结构化结论。
type Analyzer interface {
	Analyze(ctx context.Context, ref domain.ImageRef, checklist string) (Result, error)
}

// Result 是专家分析结果。
type Result struct {
	Text           string
	ModelName      string
	EstimatedTokens int64
	Duration       time.Duration
}

// UsageSink 记账视觉专家 Token（可接 TreeBudget / Usage meter）。
type UsageSink func(ctx context.Context, model string, tokens int64)

// Expert 是默认实现；ChatModel 注入后走 router.vision。
type Expert struct {
	Mode      domainvision.Mode
	Model     llm.ChatModel
	Tracer    trace.Tracer
	OnUsage   UsageSink
	Estimator func(ctx context.Context, text, model string) int
	Checklist string // 默认检查清单文案
}

// Analyze 在 expert_route 下调用视觉模型，并记录 Trace/Usage。
func (e *Expert) Analyze(ctx context.Context, ref domain.ImageRef, checklist string) (Result, error) {
	if e == nil {
		return Result{}, fmt.Errorf("vision expert not configured")
	}
	if e.Mode != domainvision.ModeExpertRoute {
		return Result{}, fmt.Errorf("vision expert only for expert_route, got %s", e.Mode)
	}
	if e.Model == nil {
		return Result{}, fmt.Errorf("vision expert model not wired")
	}
	if strings.TrimSpace(checklist) == "" {
		checklist = e.Checklist
	}
	if strings.TrimSpace(checklist) == "" {
		checklist = "layout, contrast, overflow, text_legibility"
	}
	modelName := e.Model.GetModelName()
	start := time.Now()
	var span *trace.Span
	if e.Tracer != nil {
		span = e.Tracer.StartSpan(ctx, "vision_expert", "vision:"+modelName)
	}
	var analyzeErr error
	defer func() {
		if e.Tracer != nil && span != nil {
			e.Tracer.EndSpan(ctx, span, analyzeErr)
		}
	}()

	prompt := "You are a vision expert. Inspect the attached image.\n" +
		"1) Write a clear Chinese description of what you see (scene, main objects, readable text, colors/layout).\n" +
		"2) Then on a new line return ONLY JSON {\"passed\":bool,\"defects\":[]string} for this checklist:\n" + checklist
	msg := domain.NewUserMessageWithParts(prompt, []domain.ContentPart{
		{Type: domain.ContentPartText, Text: prompt},
		{Type: domain.ContentPartImage, ImageRef: &ref},
	})
	resp, err := e.Model.Generate(ctx, []*domain.Message{msg}, nil)
	if err != nil {
		analyzeErr = err
		return Result{}, err
	}
	text := resp.TextContent()
	var tokens int64
	if e.Estimator != nil {
		tokens = int64(e.Estimator(ctx, prompt+text, modelName))
	} else {
		tokens = int64(len(prompt)+len(text)) / 4
	}
	if e.OnUsage != nil && tokens > 0 {
		e.OnUsage(ctx, modelName, tokens)
	}
	return Result{Text: text, ModelName: modelName, EstimatedTokens: tokens, Duration: time.Since(start)}, nil
}
