package collab

import (
	"context"
	"path/filepath"
)

// FilePlanDocuments 基于工作区相对路径 `.genesis/plans/<session>.md` 的实施方案存储。
type FilePlanDocuments struct {
	WorkspaceRoot string
}

// NewFilePlanDocuments 创建本地文件实施方案存储。
func NewFilePlanDocuments(workspaceRoot string) *FilePlanDocuments {
	root := filepath.Clean(workspaceRoot)
	if root == "" {
		root = "."
	}
	return &FilePlanDocuments{WorkspaceRoot: root}
}

// Write 写入或覆写实施方案。
func (d *FilePlanDocuments) Write(_ context.Context, sessionID, content string) (string, error) {
	root := "."
	if d != nil && d.WorkspaceRoot != "" {
		root = d.WorkspaceRoot
	}
	return WritePlanDocument(root, sessionID, content)
}

// Read 读取实施方案；不存在返回空内容。
func (d *FilePlanDocuments) Read(_ context.Context, sessionID string) (string, string, error) {
	root := "."
	if d != nil && d.WorkspaceRoot != "" {
		root = d.WorkspaceRoot
	}
	return ReadPlanDocument(root, sessionID)
}
