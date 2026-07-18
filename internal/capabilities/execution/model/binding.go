package model

import (
	"fmt"
	"path"
	"strings"
)

const maxExecutionIdentityLength = 256

// SameExecutionSubject 判断两个 owner 是否表示同一 Run 内可复用的派生执行主体。
func (o ExecutionOwnerRef) SameExecutionSubject(other ExecutionOwnerRef) bool {
	if o.RunID != other.RunID {
		return false
	}
	if o.TaskID == "" && o.SubAgentInstanceID == "" && o.WorkflowStepID == "" && o.MemberID == "" {
		return false
	}
	return o.TaskID == other.TaskID && o.SubAgentInstanceID == other.SubAgentInstanceID && o.WorkflowStepID == other.WorkflowStepID && o.CollaborationSpaceID == other.CollaborationSpaceID && o.MemberID == other.MemberID
}

// Subject 返回不携带授权作用域的执行主体快照。
func (o ExecutionOwnerRef) Subject() ExecutionSubjectRef {
	return ExecutionSubjectRef{TaskID: o.TaskID, SubAgentInstanceID: o.SubAgentInstanceID, WorkflowStepID: o.WorkflowStepID, CollaborationSpaceID: o.CollaborationSpaceID, MemberID: o.MemberID}
}

// ApplyTo 把主体字段应用到由控制面构造的 owner。
func (s ExecutionSubjectRef) ApplyTo(owner ExecutionOwnerRef) ExecutionOwnerRef {
	owner.TaskID = strings.TrimSpace(s.TaskID)
	owner.SubAgentInstanceID = strings.TrimSpace(s.SubAgentInstanceID)
	owner.WorkflowStepID = strings.TrimSpace(s.WorkflowStepID)
	owner.CollaborationSpaceID = strings.TrimSpace(s.CollaborationSpaceID)
	owner.MemberID = strings.TrimSpace(s.MemberID)
	return owner
}

// Empty 判断是否未声明派生执行主体。
func (s ExecutionSubjectRef) Empty() bool {
	return strings.TrimSpace(s.TaskID) == "" && strings.TrimSpace(s.SubAgentInstanceID) == "" && strings.TrimSpace(s.WorkflowStepID) == "" && strings.TrimSpace(s.MemberID) == ""
}

// Validate 校验编排主体字段，不允许残缺的 CollaborationSpace member 身份。
func (s ExecutionSubjectRef) Validate() error {
	values := []struct{ name, value string }{{"task id", s.TaskID}, {"subagent instance id", s.SubAgentInstanceID}, {"workflow step id", s.WorkflowStepID}, {"collaboration space id", s.CollaborationSpaceID}, {"member id", s.MemberID}}
	for _, value := range values {
		if value.value == "" {
			continue
		}
		if err := validateExecutionIdentity(value.name, value.value); err != nil {
			return err
		}
	}
	if (s.CollaborationSpaceID == "") != (s.MemberID == "") {
		return fmt.Errorf("collaboration space id 与 member id 必须同时提供")
	}
	return nil
}

// Validate 校验可信执行绑定的结构不变量。
func (b ExecutionBinding) Validate() error {
	if err := validateExecutionIdentity("binding id", b.ID); err != nil {
		return err
	}
	if err := validateExecutionIdentity("run id", b.Owner.RunID); err != nil {
		return err
	}
	switch b.Mode {
	case WorkspaceModeProject, WorkspaceModeTask, WorkspaceModeSession:
	default:
		return fmt.Errorf("未知 workspace mode: %q", b.Mode)
	}
	switch b.Access {
	case WorkspaceAccessReadOnly, WorkspaceAccessReadWrite:
	default:
		return fmt.Errorf("未知 workspace access: %q", b.Access)
	}
	switch b.PathPolicy {
	case "", PathPolicyStrictWorkspace, PathPolicyAdvisoryWorkspace, PathPolicyPermissionOnly:
	default:
		return fmt.Errorf("未知 path policy: %q", b.PathPolicy)
	}
	if b.Owner.ParentRunID != "" && b.Owner.ParentRunID == b.Owner.RunID {
		return fmt.Errorf("parent run id 不能与 run id 相同")
	}
	if b.Owner.SubAgentInstanceID != "" && strings.TrimSpace(b.Owner.ParentRunID) == "" {
		return fmt.Errorf("子智能体 execution binding 缺少 parent run id")
	}
	if err := b.Owner.Subject().Validate(); err != nil {
		return fmt.Errorf("execution subject 无效: %w", err)
	}
	return nil
}

// ValidateFor 校验实际目录映射是否满足 binding 所选模式的最小契约。
func (w ExecutionWorkspace) ValidateFor(binding ExecutionBinding) error {
	if strings.TrimSpace(w.WorkDir) == "" {
		return fmt.Errorf("execution workspace 缺少 work dir")
	}
	if binding.Mode != WorkspaceModeTask {
		return nil
	}
	if strings.TrimSpace(w.InputDir) == "" {
		return fmt.Errorf("task job 缺少 input dir")
	}
	if strings.TrimSpace(w.OutputDir) == "" {
		return fmt.Errorf("task job 缺少 output dir")
	}
	if strings.TrimSpace(w.TmpDir) == "" {
		return fmt.Errorf("task job 缺少 tmp dir")
	}
	paths := []struct {
		name string
		path string
	}{
		{name: "work", path: w.WorkDir},
		{name: "input", path: w.InputDir},
		{name: "output", path: w.OutputDir},
		{name: "tmp", path: w.TmpDir},
	}
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if sameWorkspacePath(paths[i].path, paths[j].path) {
				return fmt.Errorf("task job 的 %s dir 与 %s dir 不能相同", paths[i].name, paths[j].name)
			}
		}
	}
	return nil
}

func validateExecutionIdentity(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s 不能为空", name)
	}
	if trimmed != value {
		return fmt.Errorf("%s 不能包含首尾空白", name)
	}
	if len(value) > maxExecutionIdentityLength {
		return fmt.Errorf("%s 超过 %d 字节", name, maxExecutionIdentityLength)
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("%s 包含非法控制字符", name)
	}
	return nil
}

func sameWorkspacePath(left, right string) bool {
	normalize := func(value string) string {
		value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
		return path.Clean(value)
	}
	return strings.EqualFold(normalize(left), normalize(right))
}
