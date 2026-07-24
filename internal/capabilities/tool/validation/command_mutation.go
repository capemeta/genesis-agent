// Package validation 提供工具参数校验与命令状态判定。
package validation

import (
	"strings"
)

// IsCommandMutatingState 判定 Shell 命令行（支持包含 &&, ;, ||, \n, | 的多条复合命令与重定向）是否会修改系统/文件/依赖状态。
func IsCommandMutatingState(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}

	// 1. 如果包含重定向符（如 > 或 >>），说明在修改或创建文件
	if containsRedirect(cmd) {
		return true
	}

	// 2. 拆分连续复合命令（; && || \n）
	subCmds := splitCompoundCommands(cmd)
	for _, sub := range subCmds {
		if isSingleSubCommandMutating(sub) {
			return true
		}
	}

	return false
}

// containsRedirect 检查命令中是否包含写文件重定向 (> 或 >>)，过滤流重定向 2>&1
func containsRedirect(cmd string) bool {
	for i := 0; i < len(cmd); i++ {
		if cmd[i] == '>' {
			// 避免将 2>&1 等文件描述符重定向误判为写文件
			if i+1 < len(cmd) && cmd[i+1] == '&' {
				continue
			}
			if i > 0 && cmd[i-1] == '&' {
				continue
			}
			return true
		}
	}
	return false
}

// splitCompoundCommands 将复合命令行按 ; && || \n \r 引号安全的拆分为独立的子命令列表
func splitCompoundCommands(cmd string) []string {
	var subs []string
	var sb strings.Builder
	var inQuote rune
	escaped := false

	runes := []rune(cmd)
	n := len(runes)

	for i := 0; i < n; i++ {
		r := runes[i]
		if escaped {
			sb.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			sb.WriteRune(r)
			escaped = true
			continue
		}
		if inQuote != 0 {
			sb.WriteRune(r)
			if r == inQuote {
				inQuote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			inQuote = r
			sb.WriteRune(r)
			continue
		}
		if r == ';' || r == '\n' || r == '\r' {
			if s := strings.TrimSpace(sb.String()); s != "" {
				subs = append(subs, s)
			}
			sb.Reset()
			continue
		}
		if r == '&' && i+1 < n && runes[i+1] == '&' {
			if s := strings.TrimSpace(sb.String()); s != "" {
				subs = append(subs, s)
			}
			sb.Reset()
			i++
			continue
		}
		if r == '|' {
			if i+1 < n && runes[i+1] == '|' {
				i++
			}
			if s := strings.TrimSpace(sb.String()); s != "" {
				subs = append(subs, s)
			}
			sb.Reset()
			continue
		}
		sb.WriteRune(r)
	}
	if s := strings.TrimSpace(sb.String()); s != "" {
		subs = append(subs, s)
	}
	return subs
}

// isSingleSubCommandMutating 判定单条子命令是否具备状态修改性
func isSingleSubCommandMutating(subCmd string) bool {
	fields := strings.Fields(subCmd)
	if len(fields) == 0 {
		return false
	}

	// 提取可执行文件基名
	exe := strings.ToLower(fields[0])
	if idx := strings.LastIndexAny(exe, `/\`); idx >= 0 {
		exe = exe[idx+1:]
	}
	// 去掉 .exe / .cmd / .bat 后缀
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1", ".sh"} {
		if strings.HasSuffix(exe, ext) {
			exe = strings.TrimSuffix(exe, ext)
			break
		}
	}

	// A. 纯只读命令清单
	switch exe {
	case "ls", "dir", "cat", "type", "head", "tail", "grep", "find", "echo", "pwd", "whoami", "which", "where":
		return false
	}

	// B. 状态修改/安装/写操作关键字
	switch exe {
	case "pip", "pip3", "npm", "yarn", "pnpm", "bun", "apt", "apt-get", "apk", "yum":
		// pip install, npm install, etc.
		if len(fields) > 1 {
			subOp := strings.ToLower(fields[1])
			if subOp == "install" || subOp == "i" || subOp == "add" || subOp == "update" || subOp == "uninstall" || subOp == "remove" {
				return true
			}
		}
		return true

	case "mkdir", "rm", "remove", "del", "delete", "cp", "copy", "mv", "move", "touch", "chmod", "chown":
		return true

	case "git":
		if len(fields) > 1 {
			gitOp := strings.ToLower(fields[1])
			switch gitOp {
			case "commit", "checkout", "pull", "merge", "rebase", "clone", "add", "reset", "apply", "stash":
				return true
			}
		}
		return false
	}

	// C. Python / Node 脚本运行
	if exe == "python" || exe == "python3" || exe == "node" {
		fullLine := strings.ToLower(subCmd)
		// 内联代码 python -c "import ...; print(...)" / node -e "..."
		if hasInlineEvalFlag(fields) {
			if strings.Contains(fullLine, "open(") && (strings.Contains(fullLine, "'w'") || strings.Contains(fullLine, "\"w\"") || strings.Contains(fullLine, "'a'") || strings.Contains(fullLine, "\"a\"")) {
				return true
			}
			if strings.Contains(fullLine, "pip ") || strings.Contains(fullLine, "install") || strings.Contains(fullLine, "remove") || strings.Contains(fullLine, "os.mkdir") || strings.Contains(fullLine, "os.remove") {
				return true
			}
			return false
		}
		// 如果脚本名称中包含 unpack, pack, create, generate, edit, build, clean 等动作
		if strings.Contains(fullLine, "unpack") || strings.Contains(fullLine, "pack") ||
			strings.Contains(fullLine, "install") || strings.Contains(fullLine, "build") ||
			strings.Contains(fullLine, "setup.py") || strings.Contains(fullLine, "write") ||
			strings.Contains(fullLine, "create") || strings.Contains(fullLine, "clean") {
			return true
		}
	}

	// 默认非只读/不确定命令保守视作可能修改状态
	return true
}

func hasInlineEvalFlag(fields []string) bool {
	for _, f := range fields {
		fLower := strings.ToLower(f)
		if fLower == "-c" || fLower == "-e" || fLower == "--eval" || strings.HasPrefix(fLower, "-c=") || strings.HasPrefix(fLower, "-c\"") || strings.HasPrefix(fLower, "-c'") {
			return true
		}
	}
	return false
}
