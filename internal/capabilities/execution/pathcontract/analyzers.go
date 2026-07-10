package pathcontract

import (
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// ShellTextAnalyzer 对 shell 命令文本做语言无关的路径扫描。
type ShellTextAnalyzer struct{}

func (ShellTextAnalyzer) Name() string { return "shell_text" }

func (ShellTextAnalyzer) Analyze(input AnalysisInput) ([]Violation, error) {
	return violationsFromText("shell_text", "command", input.Command.Command), nil
}

// PythonSourceAnalyzer 对能可靠取得源码的 Python 命令做更深一层扫描。
type PythonSourceAnalyzer struct{}

func (PythonSourceAnalyzer) Name() string { return "python_source" }

func (PythonSourceAnalyzer) Analyze(input AnalysisInput) ([]Violation, error) {
	sources := commandSources(input.Command, commandSourceSpec{
		Language:     "python",
		Executables:  []string{"python", "python.exe", "python3", "python3.exe", "py", "py.exe"},
		InlineFlags:  []string{"-c"},
		Extensions:   []string{".py"},
		StopAtOption: true,
	})
	return sourceLiteralViolations("python_source", sources, literalSingle|literalDouble|literalTripleDouble), nil
}

// JavaScriptSourceAnalyzer 扫描 Node/JS/TS 源码字面量中的路径。
type JavaScriptSourceAnalyzer struct{}

func (JavaScriptSourceAnalyzer) Name() string { return "javascript_source" }

func (JavaScriptSourceAnalyzer) Analyze(input AnalysisInput) ([]Violation, error) {
	sources := commandSources(input.Command, commandSourceSpec{
		Language:     "javascript",
		Executables:  []string{"node", "node.exe", "nodejs", "nodejs.exe", "ts-node", "ts-node.cmd", "tsx", "tsx.cmd", "bun", "bun.exe", "deno", "deno.exe"},
		InlineFlags:  []string{"-e", "--eval"},
		Extensions:   []string{".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".mts", ".cts"},
		StopAtOption: false,
	})
	return sourceLiteralViolations("javascript_source", sources, literalSingle|literalDouble|literalBacktick), nil
}

// GoSourceAnalyzer 扫描 go run 源码字面量中的路径。
type GoSourceAnalyzer struct{}

func (GoSourceAnalyzer) Name() string { return "go_source" }

func (GoSourceAnalyzer) Analyze(input AnalysisInput) ([]Violation, error) {
	sources := commandSources(input.Command, commandSourceSpec{
		Language:    "go",
		Executables: []string{"go", "go.exe"},
		Subcommands: []string{"run"},
		Extensions:  []string{".go"},
	})
	sources = append(sources, goRunPackageSources(input.Command)...)
	return sourceLiteralViolations("go_source", sources, literalDouble|literalBacktick), nil
}

func goRunPackageSources(command execmodel.Command) []analyzedSource {
	fields := splitCommandFields(command.Command)
	var sources []analyzedSource
	for i, field := range fields {
		if !matchesExecutable(field, []string{"go", "go.exe"}) || i+1 >= len(fields) || !strings.EqualFold(fields[i+1], "run") {
			continue
		}
		for j := i + 2; j < len(fields); j++ {
			arg := fields[j]
			if strings.HasPrefix(arg, "-") || matchesExtension(arg, []string{".go"}) {
				continue
			}
			if arg == "." || strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, `.\`) {
				sources = append(sources, readSourceDir(command.Cwd, arg, "go", []string{".go"})...)
			}
		}
	}
	return sources
}

// JavaSourceAnalyzer 扫描 java/javac 源码字面量中的路径。
type JavaSourceAnalyzer struct{}

func (JavaSourceAnalyzer) Name() string { return "java_source" }

func (JavaSourceAnalyzer) Analyze(input AnalysisInput) ([]Violation, error) {
	sources := commandSources(input.Command, commandSourceSpec{
		Language:    "java",
		Executables: []string{"java", "java.exe", "javac", "javac.exe"},
		Extensions:  []string{".java"},
	})
	return sourceLiteralViolations("java_source", sources, literalDouble|literalTripleDouble), nil
}

// PowerShellSourceAnalyzer 扫描 PowerShell inline/script 源码字面量中的路径。
type PowerShellSourceAnalyzer struct{}

func (PowerShellSourceAnalyzer) Name() string { return "powershell_source" }

func (PowerShellSourceAnalyzer) Analyze(input AnalysisInput) ([]Violation, error) {
	sources := commandSources(input.Command, commandSourceSpec{
		Language:    "powershell",
		Executables: []string{"powershell", "powershell.exe", "pwsh", "pwsh.exe"},
		InlineFlags: []string{"-Command", "-CommandWithArgs", "-c"},
		FileFlags:   []string{"-File", "-f"},
		Extensions:  []string{".ps1", ".psm1"},
	})
	return wholeSourceViolations("powershell_source", sources, "#"), nil
}

// ShellScriptAnalyzer 扫描 sh/bash/zsh inline/script 源码字面量中的路径。
type ShellScriptAnalyzer struct{}

func (ShellScriptAnalyzer) Name() string { return "shell_script_source" }

func (ShellScriptAnalyzer) Analyze(input AnalysisInput) ([]Violation, error) {
	sources := commandSources(input.Command, commandSourceSpec{
		Language:     "shell",
		Executables:  []string{"sh", "sh.exe", "bash", "bash.exe", "zsh", "zsh.exe"},
		InlineFlags:  []string{"-c"},
		Extensions:   []string{".sh", ".bash", ".zsh"},
		StopAtOption: true,
	})
	return wholeSourceViolations("shell_script_source", sources, "#"), nil
}

// SkillManifestAnalyzer 扫描 Skill manifest/SKILL.md 中声明的脚本和路径。
type SkillManifestAnalyzer struct{}

func (SkillManifestAnalyzer) Name() string { return "skill_manifest" }

func (SkillManifestAnalyzer) Analyze(input AnalysisInput) ([]Violation, error) {
	sources := commandSources(input.Command, commandSourceSpec{
		Language:    "skill",
		Executables: []string{"genesis-cli", "genesis-cli.exe", "genesis", "genesis.exe"},
		Extensions:  []string{"SKILL.md", "skill.md", ".skill.yaml", ".skill.yml"},
	})
	return wholeSourceViolations("skill_manifest", sources, ""), nil
}
