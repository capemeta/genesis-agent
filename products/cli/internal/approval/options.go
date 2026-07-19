package approval

import (
	"path/filepath"
	"strings"

	"genesis-agent/internal/capabilities/approval/model"
)

// Choice 描述一次可展示的审批选项。
type Choice struct {
	Key      string
	Aliases  []string
	Label    string
	Decision model.Decision
}

// BuildChoices 按策略建议的时间作用域与资源类型组合生成审批选项。
// 文件资源在 session/project 下额外提供「本文件 / 本文件夹」正交选择。
func BuildChoices(req model.Request, result model.PolicyResult) []Choice {
	choices := []Choice{{
		Key:     "y",
		Aliases: []string{"o", "once"},
		Label:   "允许本次",
		Decision: model.Decision{
			Type:   model.DecisionApproved,
			Scope:  model.GrantScopeOnce,
			Reason: "用户允许本次操作",
		},
	}}

	fileLike := isFileAction(req)
	offerDirectory := fileLike && canOfferDirectoryMode(req)

	if supportsScope(result, req, model.GrantScopeSession) {
		if fileLike {
			choices = append(choices, Choice{
				Key:     "s",
				Aliases: []string{"session"},
				Label:   "允许本会话·本文件",
				Decision: model.Decision{
					Type:     model.DecisionApprovedForScope,
					Scope:    model.GrantScopeSession,
					PathMode: model.PathGrantExact,
					Reason:   "用户允许当前会话访问本文件",
				},
			})
			if offerDirectory {
				choices = append(choices, Choice{
					Key:     "d",
					Aliases: []string{"session-dir", "session_dir"},
					Label:   "允许本会话·本文件夹",
					Decision: model.Decision{
						Type:     model.DecisionApprovedForScope,
						Scope:    model.GrantScopeSession,
						PathMode: model.PathGrantDirectory,
						Reason:   "用户允许当前会话访问本文件夹",
					},
				})
			}
		} else {
			choices = append(choices, Choice{
				Key:     "s",
				Aliases: []string{"session"},
				Label:   "允许本会话",
				Decision: model.Decision{
					Type:   model.DecisionApprovedForScope,
					Scope:  model.GrantScopeSession,
					Reason: "用户允许当前会话",
				},
			})
		}
	}

	// project 仅对文件动作开放：当前仅文件域具备 .genesis/grants.yaml 持久化。
	if fileLike && supportsScope(result, req, model.GrantScopeProject) {
		choices = append(choices, Choice{
			Key:     "p",
			Aliases: []string{"project"},
			Label:   "允许本项目·本文件",
			Decision: model.Decision{
				Type:     model.DecisionApprovedForScope,
				Scope:    model.GrantScopeProject,
				PathMode: model.PathGrantExact,
				Reason:   "用户允许本项目访问本文件",
			},
		})
		if offerDirectory {
			choices = append(choices, Choice{
				Key:     "f",
				Aliases: []string{"project-dir", "project_dir"},
				Label:   "允许本项目·本文件夹",
				Decision: model.Decision{
					Type:     model.DecisionApprovedForScope,
					Scope:    model.GrantScopeProject,
					PathMode: model.PathGrantDirectory,
					Reason:   "用户允许本项目访问本文件夹",
				},
			})
		}
	}

	choices = append(choices,
		Choice{
			Key:     "n",
			Aliases: []string{"no", "deny"},
			Label:   "拒绝",
			Decision: model.Decision{
				Type:   model.DecisionDenied,
				Scope:  model.GrantScopeOnce,
				Reason: "用户拒绝操作",
			},
		},
		Choice{
			Key:     "a",
			Aliases: []string{"abort"},
			Label:   "中断",
			Decision: model.Decision{
				Type:   model.DecisionAbort,
				Scope:  model.GrantScopeOnce,
				Reason: "用户中断任务",
			},
		},
	)
	return choices
}

// MatchChoice 按键或别名匹配选项。
func MatchChoice(choices []Choice, input string) (Choice, bool) {
	key := strings.ToLower(strings.TrimSpace(input))
	if key == "" {
		return Choice{}, false
	}
	for _, choice := range choices {
		if key == choice.Key {
			return choice, true
		}
		for _, alias := range choice.Aliases {
			if key == alias {
				return choice, true
			}
		}
	}
	return Choice{}, false
}

// FormatPrompt 生成终端提示文案。
func FormatPrompt(choices []Choice) string {
	parts := make([]string, 0, len(choices))
	for _, choice := range choices {
		parts = append(parts, "["+strings.ToUpper(choice.Key)+"]"+choice.Label)
	}
	return "请选择 " + strings.Join(parts, " / ") + ": "
}

func isFileAction(req model.Request) bool {
	return strings.HasPrefix(string(req.Action), "file.")
}

func canOfferDirectoryMode(req model.Request) bool {
	if req.Resource.Type == "directory" {
		return false
	}
	switch req.Action {
	case model.ActionFileList, model.ActionFileWalk:
		return false
	}
	path := requestBackendPath(req)
	if path == "" {
		return false
	}
	parent := filepath.Dir(filepath.Clean(path))
	return parent != "" && parent != "." && parent != filepath.Clean(path)
}

func requestBackendPath(req model.Request) string {
	if req.Metadata != nil {
		if backend := strings.TrimSpace(req.Metadata["backend"]); backend != "" {
			return backend
		}
	}
	if req.Resource.Metadata != nil {
		if backend := strings.TrimSpace(req.Resource.Metadata["backend"]); backend != "" {
			return backend
		}
	}
	uri := req.Resource.URI
	if strings.HasPrefix(uri, "file://") {
		return strings.TrimPrefix(uri, "file://")
	}
	return uri
}
