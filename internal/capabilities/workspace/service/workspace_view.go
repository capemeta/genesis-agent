package service

import (
	"context"
	"fmt"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// WorkspaceViewBuilder 在模型开始执行前完成不可变输入快照和工作副本投影。
type WorkspaceViewBuilder struct {
	stager    workcontract.InputStager
	projector workcontract.WorkspaceViewProjector
	snapshots workcontract.InputSnapshotStore
}

func NewWorkspaceViewBuilder(stager workcontract.InputStager, projector workcontract.WorkspaceViewProjector, snapshots workcontract.InputSnapshotStore) (*WorkspaceViewBuilder, error) {
	if stager == nil || projector == nil || snapshots == nil {
		return nil, fmt.Errorf("workspace view builder 缺少 stager/projector/snapshot store")
	}
	return &WorkspaceViewBuilder{stager: stager, projector: projector, snapshots: snapshots}, nil
}

func (b *WorkspaceViewBuilder) Bind(ctx context.Context, execution workmodel.PreparedExecutionSnapshot, sources []workmodel.ResourceRef) (workmodel.InputManifest, workmodel.WorkspaceViewManifest, error) {
	inputs, err := b.stager.Stage(ctx, workcontract.StageRequest{Binding: execution.Binding, Sources: sources})
	if err != nil {
		return workmodel.InputManifest{}, workmodel.WorkspaceViewManifest{}, fmt.Errorf("创建 Run 输入快照: %w", err)
	}
	view, err := b.projector.Project(ctx, execution, inputs)
	if err != nil {
		for _, input := range inputs.Inputs {
			_ = b.snapshots.Remove(context.WithoutCancel(ctx), input.StagedPath)
		}
		return workmodel.InputManifest{}, workmodel.WorkspaceViewManifest{}, fmt.Errorf("投影 Run workspace view: %w", err)
	}
	return inputs, view, nil
}

var _ workcontract.RunInputBinder = (*WorkspaceViewBuilder)(nil)
