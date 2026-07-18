package service

import (
	"context"
	"testing"
)

func TestDefaultGatePipelineMatchesBasicGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	file := zipFile(t, map[string]string{"[Content_Types].xml": "<Types/>", "ppt/presentation.xml": "<p:presentation/>"})
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	basicKind, basicMIME, basicErr := (BasicGate{}).Validate(ctx, "deck.pptx", "", info.Size(), file)
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	pipelineKind, pipelineMIME, pipelineErr := DefaultGatePipeline().Validate(ctx, "deck.pptx", "", info.Size(), file)
	if basicErr != pipelineErr || basicKind != pipelineKind || basicMIME != pipelineMIME {
		t.Fatalf("pipeline mismatch: basic=(%s,%s,%v) pipeline=(%s,%s,%v)", basicKind, basicMIME, basicErr, pipelineKind, pipelineMIME, pipelineErr)
	}
}

func TestGatePipelineRejectsEmptyObject(t *testing.T) {
	t.Parallel()
	pipeline := NewGatePipeline("test", SizeLimitGate{})
	_, _, err := pipeline.Validate(context.Background(), "empty.txt", "text/plain", 0, nil)
	if err == nil {
		t.Fatal("expected empty artifact rejection")
	}
}
