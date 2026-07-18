// Package approval 提供 CLI 产品侧的审批交互适配。
package approval

import (
	"context"
	"sync"

	"genesis-agent/internal/capabilities/approval/model"
	tea "github.com/charmbracelet/bubbletea"
)

// ApprovalRequiredMsg 审批挂起消息，供 TUI 捕获并在主更新循环中渲染卡片与拦截键盘
type ApprovalRequiredMsg struct {
	Request  model.Request
	Policy   model.PolicyResult
	ResultCh chan<- model.Decision
}

// TUIApprovalRequester 实现交互 TUI (Bubble Tea) 下的非阻塞同步挂起审批。
type TUIApprovalRequester struct {
	program *tea.Program
	mu      sync.Mutex
}

var GlobalTUIRequester = &TUIApprovalRequester{}

// SetProgram 注册当前运行的 Bubble Tea Program 实例
func (r *TUIApprovalRequester) SetProgram(p *tea.Program) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.program = p
}

// RequestApproval 阻塞式挂起等待用户从 TUI 键盘返回决策
func (r *TUIApprovalRequester) RequestApproval(ctx context.Context, req model.Request, result model.PolicyResult) (model.Decision, error) {
	r.mu.Lock()
	p := r.program
	r.mu.Unlock()

	if p == nil {
		return model.Decision{
			Type:   model.DecisionDenied,
			Reason: "TUI 审批组件未就绪",
		}, nil
	}

	decisionCh := make(chan model.Decision, 1)

	// 发送审批事件通知 TUI 主线程
	p.Send(ApprovalRequiredMsg{
		Request:  req,
		Policy:   result,
		ResultCh: decisionCh,
	})

	// 阻塞等待用户从 TUI 侧写入决策
	select {
	case <-ctx.Done():
		// TUI 的 abort 会先投递显式决策再取消 Run。两者同时就绪时优先保留
		// 用户语义与审批审计，避免退化成无法区分来源的 context cancelled。
		select {
		case dec := <-decisionCh:
			return dec, nil
		default:
		}
		return model.Decision{Type: model.DecisionDenied, Reason: "context cancelled"}, ctx.Err()
	case dec := <-decisionCh:
		return dec, nil
	}
}
