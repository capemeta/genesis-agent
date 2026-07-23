package prompt

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// EnvironmentContext 是每轮对模型可见的有界运行环境快照。
// 它只描述已经验证的能力，不应根据操作系统猜测远程沙箱能力。
type EnvironmentContext struct {
	OS                 string
	HostCommandTool    string
	DefaultShell       string
	DefaultShellPath   string
	SupportedShells    []string
	SandboxMode        string
	SandboxProvider    string
	SandboxCommandTool string
	ExternalApproval   bool
}

// NewEnvironmentContextInjector 创建运行环境上下文注入器。
func NewEnvironmentContextInjector(environment EnvironmentContext) ContextInjector {
	return ContextInjectorFunc(func(ctx context.Context, req BuildRequest) (Fragment, error) {
		if err := ctx.Err(); err != nil {
			return Fragment{}, err
		}
		_ = req
		contents := renderEnvironmentContext(ctx, environment)
		if contents == "" {
			return Fragment{}, nil
		}
		return Fragment{Name: "environment_context", Contents: contents}, nil
	})
}

func renderEnvironmentContext(ctx context.Context, environment EnvironmentContext) string {
	var lines []string
	appendValue := func(name, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		lines = append(lines, fmt.Sprintf("<%s>%s</%s>", name, xmlEscape(value), name))
	}
	hostOS := strings.TrimSpace(environment.OS)
	if hostOS != "" {
		lines = append(lines, fmt.Sprintf("<host_environment%s os=\"%s\" />", toolAttribute(environment.HostCommandTool), xmlEscape(hostOS)))
	}
	if prepared, ok := workcontract.PreparedRunFromContext(ctx); ok {
		lines = append(lines, renderWorkspaceContext(prepared)...)
	}
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
		available := strings.TrimSpace(environment.SandboxProvider) != "" && environment.SandboxMode != string(execmodel.SandboxDisabled)
		lines = append(lines, fmt.Sprintf(
			"<sandbox_environment available=\"%t\"%s mode=\"%s\" provider=\"%s\" cwd=\"/workspace\" />",
			available,
			toolAttribute(environment.SandboxCommandTool),
			xmlEscape(environment.SandboxMode),
			xmlEscape(environment.SandboxProvider),
		))
		if available && strings.TrimSpace(environment.HostCommandTool) != "" && strings.TrimSpace(environment.SandboxCommandTool) != "" {
			hostRule := hostOSRuleText(environment.HostCommandTool, hostOS)
			ruleText := fmt.Sprintf(`[环境差异与命令执行规则]:
%s
2. %s / run_skill_command 工具 (Linux 沙箱): 专门在隔离容器环境 (/workspace) 中运行第三方工具、数据提取与风险代码。必须使用 POSIX/Linux Shell 语法与正斜线 / 路径。
3. [隔离与文件传递]: 宿主与沙箱文件系统隔离，沙箱仅接收 inputs 显式传入的输入文件，并自动将 OUTPUT_DIR 中的结果发布为交付物。
4. [最佳实践]: 进行文件读写、发现与查找时，请优先使用通用工具 (read_file, write_file, list_dir, glob, grep)，避免因跨 OS 命令行语法差异导致错误。`,
				hostRule, environment.SandboxCommandTool,
			)
			lines = append(lines, fmt.Sprintf("<environment_rule>\n%s\n</environment_rule>", xmlEscape(ruleText)))
		}
	}
	if environment.ExternalApproval {
		lines = append(lines, "<filesystem external_access_requires_approval=\"true\" />")
	}
	return strings.Join(lines, "\n")
}

func toolAttribute(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf(" tool=\"%s\"", xmlEscape(name))
}

func renderWorkspaceContext(prepared workmodel.PreparedRun) []string {
	binding := prepared.Execution.Binding
	persistence := "run_isolated"
	projectChangesPersist := false
	switch binding.Mode {
	case execmodel.WorkspaceModeProject:
		persistence = "project"
		projectChangesPersist = true
	case execmodel.WorkspaceModeSession:
		persistence = "session"
	}
	lines := []string{fmt.Sprintf(
		"<workspace mode=\"%s\" access=\"%s\" root=\".\" persistence=\"%s\" project_changes_persist=\"%t\" />",
		xmlEscape(string(binding.Mode)), xmlEscape(string(binding.Access)), persistence, projectChangesPersist,
	)}
	if len(prepared.Manifest.View.Entries) > 0 {
		lines = append(lines, "<bound_inputs>")
		for _, entry := range prepared.Manifest.View.Entries {
			lines = append(lines, fmt.Sprintf("<file path=\"%s\" access=\"%s\" />", xmlEscape(string(entry.Path)), xmlEscape(entry.Access)))
		}
		lines = append(lines, "</bound_inputs>")
	}
	if binding.Mode == execmodel.WorkspaceModeTask {
		lines = append(lines, "<workspace_rule>当前为隔离任务；相对路径只解析到本 Run 根，写入不会修改项目。仅 bound_inputs 中列出的项目资源已投影为工作副本。</workspace_rule>")
	} else if binding.Mode == execmodel.WorkspaceModeProject {
		lines = append(lines, "<workspace_rule>当前为项目开发；相对路径根就是已授权项目根，修改会直接持久化到项目。</workspace_rule>")
	} else {
		lines = append(lines, "<workspace_rule>当前为会话工作区；相对路径在会话范围持久化，不会直接修改项目。</workspace_rule>")
	}
	return lines
}

func xmlEscape(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func hostOSRuleText(toolName, osName string) string {
	switch strings.ToLower(strings.TrimSpace(osName)) {
	case "windows":
		return fmt.Sprintf("1. %s 工具 (宿主系统: Windows): 专门在宿主 Windows 上运行本地构建、测试和系统命令。请使用 PowerShell / cmd 语法（支持 .exe 扩展名与 PowerShell 常用命令），路径可使用 \\ 或 /。", toolName)
	case "darwin", "macos":
		return fmt.Sprintf("1. %s 工具 (宿主系统: macOS): 专门在宿主 macOS 上运行本地构建、测试和系统命令。请使用 Zsh / Bash POSIX 标准命令，路径必须使用正斜线 /。", toolName)
	case "linux":
		return fmt.Sprintf("1. %s 工具 (宿主系统: Linux): 专门在宿主 Linux 上运行本地构建、测试和系统命令。请使用 Bash POSIX 标准命令，路径必须使用正斜线 /。", toolName)
	default:
		if osName == "" {
			osName = "未指定"
		}
		return fmt.Sprintf("1. %s 工具 (宿主系统: %s): 专门在宿主机上运行本地构建、测试和系统命令。请使用符合该宿主 Shell 语法的命令。", toolName, osName)
	}
}
