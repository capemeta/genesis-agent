package prompt

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
)

// EnvironmentContext 是每轮对模型可见的有界运行环境快照。
// 它只描述已经验证的能力，不应根据操作系统猜测远程沙箱能力。
type EnvironmentContext struct {
	OS               string
	Cwd              string
	DefaultShell     string
	DefaultShellPath string
	SupportedShells  []string
	SandboxMode      string
	SandboxProvider  string
	ExternalApproval bool
}

// NewEnvironmentContextInjector 创建运行环境上下文注入器。
func NewEnvironmentContextInjector(environment EnvironmentContext) ContextInjector {
	return ContextInjectorFunc(func(ctx context.Context, req BuildRequest) (Fragment, error) {
		if err := ctx.Err(); err != nil {
			return Fragment{}, err
		}
		_ = req
		contents := renderEnvironmentContext(environment)
		if contents == "" {
			return Fragment{}, nil
		}
		return Fragment{Name: "environment_context", Contents: contents}, nil
	})
}

func renderEnvironmentContext(environment EnvironmentContext) string {
	var lines []string
	appendValue := func(name, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		lines = append(lines, fmt.Sprintf("<%s>%s</%s>", name, xmlEscape(value), name))
	}
	appendValue("os", environment.OS)
	appendValue("cwd", environment.Cwd)
	if strings.TrimSpace(environment.DefaultShell) != "" {
		attrs := fmt.Sprintf(" name=\"%s\"", xmlEscape(environment.DefaultShell))
		if path := strings.TrimSpace(environment.DefaultShellPath); path != "" {
			attrs += fmt.Sprintf(" path=\"%s\"", xmlEscape(path))
		}
		lines = append(lines, "<default_shell"+attrs+" />")
	}
	if len(environment.SupportedShells) > 0 {
		clean := make([]string, 0, len(environment.SupportedShells))
		seen := make(map[string]struct{}, len(environment.SupportedShells))
		for _, shell := range environment.SupportedShells {
			shell = strings.TrimSpace(shell)
			if shell == "" {
				continue
			}
			key := strings.ToLower(shell)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			clean = append(clean, shell)
		}
		appendValue("supported_shells", strings.Join(clean, ","))
	}
	if strings.TrimSpace(environment.SandboxMode) != "" || strings.TrimSpace(environment.SandboxProvider) != "" {
		lines = append(lines, fmt.Sprintf(
			"<sandbox mode=\"%s\" provider=\"%s\" />",
			xmlEscape(environment.SandboxMode),
			xmlEscape(environment.SandboxProvider),
		))
	}
	if environment.ExternalApproval {
		lines = append(lines, "<filesystem external_access_requires_approval=\"true\" />")
	}
	return strings.Join(lines, "\n")
}

func xmlEscape(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}
