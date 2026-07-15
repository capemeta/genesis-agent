package service

import (
	"fmt"
	"strings"

	"genesis-agent/internal/capabilities/hook/model"
)

func aggregate(event model.Event, decisions []model.Decision) model.AggregateResult {
	result := model.AggregateResult{}
	for _, decision := range decisions {
		if decision.Err != nil {
			result.Warnings = append(result.Warnings, decision.Err.Error())
		}
		if msg := strings.TrimSpace(decision.SystemMessage); msg != "" {
			result.SystemMessages = append(result.SystemMessages, msg)
		}
		if context := strings.TrimSpace(decision.AdditionalContext); context != "" {
			result.AdditionalContext = append(result.AdditionalContext, context)
		}
		if len(decision.UpdatedInput) > 0 {
			if result.UpdatedInput == nil {
				result.UpdatedInput = make(map[string]any)
			}
			for key, value := range decision.UpdatedInput {
				result.UpdatedInput[key] = value
			}
		}
		permission := strings.ToLower(strings.TrimSpace(decision.PermissionDecision))
		if permission == "approve" {
			permission = "allow"
		}
		if permission == "block" {
			permission = "deny"
		}
		if permission == "ask" {
			result.NeedApproval = true
		}
		blocked := decision.ExitCode == 2 || permission == "deny" || !decision.Continue
		if blocked && event.Name.IsBlockingEvent() && !result.Blocked {
			result.Blocked = true
			result.BlockReason = strings.TrimSpace(decision.Reason)
			if result.BlockReason == "" {
				result.BlockReason = fmt.Sprintf("Hook 阻断了 %s", event.Name)
			}
		}
	}
	return result
}
