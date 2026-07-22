package approval

import (
	"fmt"
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
				Label:   "允许本会话·本文件 (本次对话有效)",
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
					Label:   "允许本会话·本文件夹 (本次对话有效)",
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
				Label:   "允许本会话 (本次对话有效)",
				Decision: model.Decision{
					Type:   model.DecisionApprovedForScope,
					Scope:  model.GrantScopeSession,
					Reason: "用户允许当前会话",
				},
			})
		}
	}

	// project 仅对文件动作开放：项目级授权写入工作区持久化保存
	if fileLike && supportsScope(result, req, model.GrantScopeProject) {
		choices = append(choices, Choice{
			Key:     "p",
			Aliases: []string{"project"},
			Label:   "允许本项目·本文件 (记住选择，总是允许)",
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
				Label:   "允许本项目·本文件夹 (记住选择，总是允许)",
				Decision: model.Decision{
					Type:     model.DecisionApprovedForScope,
					Scope:    model.GrantScopeProject,
					PathMode: model.PathGrantDirectory,
					Reason:   "用户允许本项目访问本文件夹",
				},
			})
		}
	}

	choices = append(choices, Choice{
		Key:     "n",
		Aliases: []string{"no", "deny"},
		Label:   "拒绝",
		Decision: model.Decision{
			Type:   model.DecisionDenied,
			Scope:  model.GrantScopeOnce,
			Reason: "用户拒绝本次操作",
		},
	})

	return choices
}

// FormatPrompt 格式化终端输入的快捷键提示文案。
func FormatPrompt(choices []Choice) string {
	parts := make([]string, 0, len(choices))
	for _, c := range choices {
		parts = append(parts, fmt.Sprintf("[%s]%s", strings.ToUpper(c.Key), c.Label))
	}
	return fmt.Sprintf("请选择 %s: ", strings.Join(parts, " / "))
}

// MatchChoice 匹配用户输入的快捷键。
func MatchChoice(choices []Choice, input string) (Choice, bool) {
	norm := strings.TrimSpace(strings.ToLower(input))
	if norm == "" {
		return Choice{}, false
	}
	for _, c := range choices {
		if norm == strings.ToLower(c.Key) {
			return c, true
		}
		for _, alias := range c.Aliases {
			if norm == strings.ToLower(alias) {
				return c, true
			}
		}
	}
	return Choice{}, false
}

func isFileAction(req model.Request) bool {
	return strings.HasPrefix(string(req.Action), "file.")
}

func canOfferDirectoryMode(req model.Request) bool {
	if req.Resource.Type == "directory" {
		return false
	}
	res := strings.TrimSpace(req.Resource.Display)
	if res == "" {
		res = strings.TrimSpace(req.Resource.URI)
	}
	if res == "" || res == "." {
		return false
	}
	dir := filepath.Dir(res)
	return dir != "." && dir != "/" && dir != "\\"
}
