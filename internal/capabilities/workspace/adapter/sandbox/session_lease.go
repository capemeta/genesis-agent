package sandbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// SandboxRenewer 续租远程 sandbox（Job 路径）；返回服务端确认的新到期时间。
type SandboxRenewer interface {
	RenewSandbox(ctx context.Context, sandboxID string) (time.Time, error)
}

// SessionRenewer 续期命名 Session（同时延长底层 sandbox lease），对齐 genesis-sandbox SDK。
// session-file 发布/读取续租必须使用该接口，不得仅 RenewSandbox。
type SessionRenewer interface {
	RenewSession(ctx context.Context, sessionID string) (time.Time, error)
}

// SessionLeaseKeeper 在发布前对 session-file leased 资源尽力续租。
// locator/descriptor 的 ExpiresAt 仍是登记快照；续租成功只证明 live session 仍可读。
type SessionLeaseKeeper struct {
	locators RemoteLocatorStore
	renewer  SessionRenewer
	now      func() time.Time
}

func NewSessionLeaseKeeper(locators RemoteLocatorStore, renewer SessionRenewer) (*SessionLeaseKeeper, error) {
	if locators == nil || renewer == nil {
		return nil, fmt.Errorf("session lease keeper 缺少 locators/renewer")
	}
	return &SessionLeaseKeeper{locators: locators, renewer: renewer, now: time.Now}, nil
}

func (k *SessionLeaseKeeper) EnsureLeasedReadable(ctx context.Context, descriptor workmodel.ProducedResourceDescriptor) error {
	if descriptor.Availability != workmodel.ResourceAvailabilityLeased {
		return nil
	}
	if descriptor.Source.Authority != RemoteExecutorAuthority || descriptor.Source.Scheme != SessionFileScheme {
		if descriptor.ExpiresAt == nil || !descriptor.ExpiresAt.After(k.now()) {
			return workcontract.NewError(workcontract.ErrCodeProducedResourceExpired, fmt.Errorf("produced resource lease 已过期"))
		}
		return nil
	}
	locator, err := k.locators.Get(ctx, descriptor.Source.ID, descriptor.Source.Scope)
	if err != nil {
		return err
	}
	if _, err := renewSessionFileLease(ctx, k.renewer, locator.Workspace); err != nil {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceExpired, fmt.Errorf("续租 session 失败: %w", err))
	}
	return nil
}

// renewSessionFileLease 调用 POST /v1/sessions/{id}/renew，同时续 Session 与底层 lease。
func renewSessionFileLease(ctx context.Context, renewer SessionRenewer, workspace sandboxcontract.WorkspaceRef) (time.Time, error) {
	if renewer == nil {
		return time.Time{}, fmt.Errorf("renewer 未配置")
	}
	sessionID := strings.TrimSpace(workspace.Metadata["session_id"])
	if sessionID == "" {
		sessionID = strings.TrimSpace(workspace.ID)
	}
	if sessionID == "" {
		return time.Time{}, fmt.Errorf("session-file locator 缺少 session_id")
	}
	return renewer.RenewSession(ctx, sessionID)
}
