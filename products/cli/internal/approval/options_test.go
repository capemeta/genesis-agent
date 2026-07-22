package approval

import (
	"testing"

	"genesis-agent/internal/capabilities/approval/model"
)

func TestBuildChoicesFileWithProjectAndDirectory(t *testing.T) {
	req := model.Request{
		Action: model.ActionFileRead,
		Resource: model.Resource{
			Type:    "file",
			URI:     "file:///tmp/dir/a.txt",
			Display: "/tmp/dir/a.txt",
			Metadata: map[string]string{
				"backend": `/tmp/dir/a.txt`,
			},
		},
		Metadata: map[string]string{"backend": `/tmp/dir/a.txt`},
		SuggestedScopes: []model.GrantScope{
			model.GrantScopeOnce,
			model.GrantScopeSession,
			model.GrantScopeProject,
		},
	}
	result := model.PolicyResult{SuggestedScopes: req.SuggestedScopes}
	choices := BuildChoices(req, result)
	keys := map[string]bool{}
	for _, c := range choices {
		keys[c.Key] = true
	}
	for _, want := range []string{"y", "s", "d", "p", "f", "n"} {
		if !keys[want] {
			t.Fatalf("missing choice %q in %+v", want, keys)
		}
	}
	choice, ok := MatchChoice(choices, "f")
	if !ok || choice.Decision.Scope != model.GrantScopeProject || choice.Decision.PathMode != model.PathGrantDirectory {
		t.Fatalf("project directory choice = %+v ok=%v", choice.Decision, ok)
	}
}

func TestBuildChoicesNonFileOmitsProject(t *testing.T) {
	req := model.Request{
		Action: model.ActionCommandExec,
		SuggestedScopes: []model.GrantScope{
			model.GrantScopeOnce,
			model.GrantScopeSession,
			model.GrantScopeProject,
		},
	}
	choices := BuildChoices(req, model.PolicyResult{SuggestedScopes: req.SuggestedScopes})
	for _, c := range choices {
		if c.Key == "p" || c.Key == "f" || c.Decision.Scope == model.GrantScopeProject {
			t.Fatalf("non-file must not offer project grant, got %+v", c)
		}
	}
}

func TestBuildChoicesDirectoryResourceSkipsPathSplit(t *testing.T) {
	req := model.Request{
		Action: model.ActionFileList,
		Resource: model.Resource{
			Type: "directory",
			URI:  "file:///tmp/dir",
			Metadata: map[string]string{
				"backend": `/tmp/dir`,
			},
		},
		Metadata:        map[string]string{"backend": `/tmp/dir`},
		SuggestedScopes: []model.GrantScope{model.GrantScopeOnce, model.GrantScopeSession, model.GrantScopeProject},
	}
	choices := BuildChoices(req, model.PolicyResult{SuggestedScopes: req.SuggestedScopes})
	for _, c := range choices {
		if c.Key == "d" || c.Key == "f" {
			t.Fatalf("directory resource should not offer path split, got %q", c.Key)
		}
	}
}
