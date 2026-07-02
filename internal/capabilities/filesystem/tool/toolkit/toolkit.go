// Package toolkit 提供文件系统工具共享装配。
package toolkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/freshness"
	"genesis-agent/internal/capabilities/filesystem/model"
	"genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/tool/scheduler"
)

// Deps 是文件工具共享依赖。
type Deps struct {
	Resolver  fscontract.PathResolver
	Backend   fscontract.FileSystemBackend
	Approval  approvalcontract.Service
	Freshness freshness.Tracker
	Locker    scheduler.ResourceLocker
}

// Validate 校验依赖。
func (d Deps) Validate() error {
	if d.Resolver == nil {
		return fmt.Errorf("PathResolver未配置")
	}
	if d.Backend == nil {
		return fmt.Errorf("FileSystemBackend未配置")
	}
	if d.Approval == nil {
		return fmt.Errorf("ApprovalService未配置")
	}
	if d.Freshness == nil {
		return fmt.Errorf("FreshnessTracker未配置")
	}
	if d.Locker == nil {
		return fmt.Errorf("ResourceLocker未配置")
	}
	return nil
}

// DecodeParams 严格解析工具参数，避免模型拼错字段后被静默忽略。
func DecodeParams(params string, dst any) error {
	decoder := json.NewDecoder(strings.NewReader(params))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("参数解析失败: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("参数只能包含一个JSON对象")
	}
	return nil
}

// ResolveRequire 解析路径并通过通用 approval 授权。
func ResolveRequire(ctx context.Context, deps Deps, toolName string, raw string, op permission.Operation, opts fscontract.ResolveOptions) (model.ResolvedPath, error) {
	path, err := deps.Resolver.Resolve(ctx, model.PathRef{Raw: raw}, opts)
	if err != nil {
		return model.ResolvedPath{}, err
	}
	req := permission.BuildApprovalRequest(toolName, op, path)
	decision, err := deps.Approval.Authorize(ctx, req)
	if err != nil {
		return model.ResolvedPath{}, err
	}
	if !isApproved(decision) {
		return model.ResolvedPath{}, approvalDeniedError(path, decision)
	}
	return path, nil
}

func isApproved(decision approvalmodel.Decision) bool {
	return decision.Type == approvalmodel.DecisionApproved || decision.Type == approvalmodel.DecisionApprovedForScope
}

func approvalDeniedError(path model.ResolvedPath, decision approvalmodel.Decision) error {
	reason := decision.Reason
	if reason == "" {
		reason = string(decision.Type)
	}
	return fscontract.NewError(fscontract.ErrCodePermissionDenied, path.DisplayPath, fmt.Errorf("approval %s: %s", decision.Type, reason))
}

// Acquire 获取工具锁。
func Acquire(ctx context.Context, locker scheduler.ResourceLocker, locks []scheduler.ResourceLock) (func(), error) {
	if locker == nil {
		return func() {}, nil
	}
	return locker.Acquire(ctx, locks)
}

// FileLockKey 生成文件锁 key。
func FileLockKey(path model.ResolvedPath) string {
	if path.BackendPath != "" {
		return path.BackendPath
	}
	return path.WorkspaceRel
}

// WorkspaceLockKey 生成 workspace 锁 key。
func WorkspaceLockKey(path model.ResolvedPath) string {
	if path.WorkspaceID != "" {
		return path.WorkspaceID
	}
	return "default"
}

// HashBytes 计算 sha256 hex。
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ToJSON 将工具结果编码为 JSON 字符串。
func ToJSON(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化工具结果失败: %w", err)
	}
	return string(data), nil
}
