package service

import "strings"

const (
	ValidatorContentQA   = "content-qa/v1"
	ValidatorRenderProof = "render-proof/v1"
	ValidatorVisualQA    = "visual-qa/v1"
)

// ClassifySkillQACommand 将 skill QA 命令归类为 content 或 render 证据（永不返回 visual-qa）。
func ClassifySkillQACommand(command string) string {
	c := strings.ToLower(command)
	switch {
	case strings.Contains(c, "thumbnail"), strings.Contains(c, "pdftoppm"), strings.Contains(c, "soffice"):
		return ValidatorRenderProof
	default:
		return ValidatorContentQA
	}
}
