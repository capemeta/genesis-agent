package model

import "testing"

func TestExecutionBindingValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		binding ExecutionBinding
		wantErr bool
	}{
		{
			name: "project binding",
			binding: ExecutionBinding{ID: "binding-1", Mode: WorkspaceModeProject, Access: WorkspaceAccessReadWrite,
				Owner: ExecutionOwnerRef{RunID: "run-1"}},
		},
		{
			name: "task binding",
			binding: ExecutionBinding{ID: "binding-2", Mode: WorkspaceModeTask, Access: WorkspaceAccessReadOnly,
				Owner: ExecutionOwnerRef{RunID: "run-2"}},
		},
		{
			name: "missing id",
			binding: ExecutionBinding{Mode: WorkspaceModeProject, Access: WorkspaceAccessReadWrite,
				Owner: ExecutionOwnerRef{RunID: "run-1"}},
			wantErr: true,
		},
		{
			name: "old mode rejected",
			binding: ExecutionBinding{ID: "binding-3", Mode: WorkspaceMode("invalid_workspace_mode"), Access: WorkspaceAccessReadWrite,
				Owner: ExecutionOwnerRef{RunID: "run-3"}},
			wantErr: true,
		},
		{
			name: "subagent without parent",
			binding: ExecutionBinding{ID: "binding-4", Mode: WorkspaceModeSession, Access: WorkspaceAccessReadOnly,
				Owner: ExecutionOwnerRef{RunID: "child-run", SubAgentInstanceID: "agent-1"}},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.binding.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestExecutionWorkspaceValidateForTaskRequiresSeparatedDirectories(t *testing.T) {
	t.Parallel()
	binding := ExecutionBinding{ID: "binding-task", Mode: WorkspaceModeTask, Access: WorkspaceAccessReadWrite,
		Owner: ExecutionOwnerRef{RunID: "run-task"}}

	valid := ExecutionWorkspace{WorkDir: "/workspace/work", InputDir: "/workspace/input", OutputDir: "/workspace/output", TmpDir: "/workspace/tmp"}
	if err := valid.ValidateFor(binding); err != nil {
		t.Fatalf("ValidateFor() error = %v", err)
	}

	invalid := valid
	invalid.OutputDir = invalid.WorkDir
	if err := invalid.ValidateFor(binding); err == nil {
		t.Fatal("ValidateFor() error = nil, want duplicated directory error")
	}
}

func TestExecutionSubjectRequiresCompleteCollaborationIdentity(t *testing.T) {
	t.Parallel()
	for _, subject := range []ExecutionSubjectRef{
		{CollaborationSpaceID: "space"},
		{MemberID: "member"},
	} {
		if err := subject.Validate(); err == nil {
			t.Fatalf("incomplete collaboration subject accepted: %+v", subject)
		}
	}
	if err := (ExecutionSubjectRef{CollaborationSpaceID: "space", MemberID: "member"}).Validate(); err != nil {
		t.Fatalf("complete collaboration subject rejected: %v", err)
	}
}
