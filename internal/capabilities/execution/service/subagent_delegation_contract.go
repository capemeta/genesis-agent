package service

import "fmt"

// ResourceBinding 定义输入/输出文件的资源绑定快照。
type ResourceBinding struct {
	LogicalPath  string `json:"logical_path"`  // 相对逻辑路径 (如 src/main.go)
	PhysicalPath string `json:"physical_path"` // 宿主物理绝对路径 (如 D:\workspace\...)
	FileType     string `json:"file_type"`     // file, directory, artifact
}

// SubAgentDelegationRequest 定义主 Agent 向沙箱 Worker 子 Agent 发起委派的请求契约。
type SubAgentDelegationRequest struct {
	TaskID        string            `json:"task_id"`        // 唯一任务 ID
	Goal          string            `json:"goal"`           // 任务目标描述
	SkillName     string            `json:"skill_name"`     // 关联的 Skill 名称
	InputManifest []ResourceBinding `json:"input_manifest"` // 边界穿越 1: 输入文件快照
	SessionID     string            `json:"session_id"`     // 关联的远程沙箱 Session ID
	MaxTurns      int               `json:"max_turns"`      // 子 Agent 最大交互轮数
}

// SubAgentDelegationResult 定义沙箱 Worker 子 Agent 执行完毕后交还主 Agent 的结果契约。
type SubAgentDelegationResult struct {
	TaskID          string            `json:"task_id"`          // 对应的任务 ID
	Status          string            `json:"status"`           // success, failed, timeout
	Summary         string            `json:"summary"`          // 给主 Agent 的精简汇报
	ProducedOutputs []ResourceBinding `json:"produced_outputs"` // 边界穿越 2: 最终交付物成品列表
	WorkspacePatch  *WorkspacePatch   `json:"workspace_patch"`  // 增量 Git Diff 补丁
	Warnings        []string          `json:"warnings"`         // 运行审计 Warning 说明
}

// ValidateRequest 校验委派请求格式规范。
func (r *SubAgentDelegationRequest) ValidateRequest() error {
	if r.TaskID == "" {
		return fmt.Errorf("task_id 不能为空")
	}
	if r.Goal == "" {
		return fmt.Errorf("goal 任务目标不能为空")
	}
	return nil
}
