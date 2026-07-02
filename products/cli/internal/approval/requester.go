// Package approval 提供 CLI 产品侧的审批交互适配。
package approval

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"genesis-agent/internal/capabilities/approval/model"
)

// TerminalRequester 通过终端同步确认 ask 决策。
type TerminalRequester struct {
	in  *bufio.Reader
	out io.Writer
}

// NewTerminalRequester 创建终端审批 requester。
func NewTerminalRequester(in io.Reader, out io.Writer) *TerminalRequester {
	var reader *bufio.Reader
	if in != nil {
		reader = bufio.NewReader(in)
	}
	return &TerminalRequester{in: reader, out: out}
}

// RequestApproval 请求用户确认本次高风险操作。
func (r *TerminalRequester) RequestApproval(ctx context.Context, req model.Request, result model.PolicyResult) (model.Decision, error) {
	if err := ctx.Err(); err != nil {
		return model.Decision{}, err
	}
	if r == nil || r.in == nil || r.out == nil {
		return model.Decision{
			Type:   model.DecisionDenied,
			Scope:  model.GrantScopeOnce,
			Reason: "审批终端未初始化",
		}, nil
	}

	allowSession := supportsScope(result, req, model.GrantScopeSession)
	r.printRequest(req, result)
	for {
		if err := ctx.Err(); err != nil {
			return model.Decision{}, err
		}
		r.printPrompt(allowSession)
		line, err := r.in.ReadString('\n')
		if err != nil && !(errors.Is(err, io.EOF) && strings.TrimSpace(line) != "") {
			if errors.Is(err, io.EOF) {
				return model.Decision{
					Type:   model.DecisionDenied,
					Scope:  model.GrantScopeOnce,
					Reason: "未收到用户确认",
				}, nil
			}
			return model.Decision{}, fmt.Errorf("读取审批输入失败: %w", err)
		}

		switch strings.ToLower(strings.TrimSpace(line)) {
		case "o", "once":
			return model.Decision{Type: model.DecisionApproved, Scope: model.GrantScopeOnce, Reason: "用户允许本次操作"}, nil
		case "s", "session":
			if allowSession {
				return model.Decision{Type: model.DecisionApprovedForScope, Scope: model.GrantScopeSession, Reason: "用户允许当前会话"}, nil
			}
		case "n", "no", "deny":
			return model.Decision{Type: model.DecisionDenied, Scope: model.GrantScopeOnce, Reason: "用户拒绝操作"}, nil
		case "a", "abort":
			return model.Decision{Type: model.DecisionAbort, Scope: model.GrantScopeOnce, Reason: "用户中断任务"}, nil
		}
		r.printInvalidInput(allowSession)
	}
}

func (r *TerminalRequester) printRequest(req model.Request, result model.PolicyResult) {
	risk := result.Risk
	if risk == "" {
		risk = req.Risk
	}
	reason := result.Reason
	if reason == "" {
		reason = req.Reason
	}
	display := req.Resource.Display
	if display == "" {
		display = req.Resource.URI
	}

	fmt.Fprintln(r.out)
	fmt.Fprintln(r.out, "需要确认操作")
	fmt.Fprintf(r.out, "  工具: %s\n", req.ToolName)
	fmt.Fprintf(r.out, "  动作: %s\n", req.Action)
	fmt.Fprintf(r.out, "  资源: %s\n", display)
	if risk != "" {
		fmt.Fprintf(r.out, "  风险: %s\n", risk)
	}
	if reason != "" {
		fmt.Fprintf(r.out, "  原因: %s\n", reason)
	}
	fmt.Fprintln(r.out)
}

func (r *TerminalRequester) printPrompt(allowSession bool) {
	if allowSession {
		fmt.Fprint(r.out, "请选择 [o]允许本次 / [s]允许本会话 / [n]拒绝 / [a]中断: ")
		return
	}
	fmt.Fprint(r.out, "请选择 [o]允许本次 / [n]拒绝 / [a]中断: ")
}

func (r *TerminalRequester) printInvalidInput(allowSession bool) {
	if allowSession {
		fmt.Fprintln(r.out, "输入无效，请输入 o、s、n 或 a。")
		return
	}
	fmt.Fprintln(r.out, "输入无效，请输入 o、n 或 a。")
}

func supportsScope(result model.PolicyResult, req model.Request, scope model.GrantScope) bool {
	if containsScope(result.SuggestedScopes, scope) {
		return true
	}
	return len(result.SuggestedScopes) == 0 && containsScope(req.SuggestedScopes, scope)
}

func containsScope(scopes []model.GrantScope, scope model.GrantScope) bool {
	for _, candidate := range scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}
