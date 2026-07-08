// Package pathcontract 校验代码执行的路径契约。
package pathcontract

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

const maxAnalyzedSourceBytes = 512 * 1024

// Severity 描述路径契约诊断级别。
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Violation 描述一条可修复路径契约问题。
type Violation struct {
	Analyzer string
	Severity Severity
	Fragment string
	Location string
	Reason   string
	Fix      string
}

// AnalysisInput 是路径分析器的输入。
type AnalysisInput struct {
	Command execmodel.Command
	Options execcontract.RunOptions
}

// Analyzer 是一个可插拔路径契约分析器。
//
// 分析器只提供执行前质量门诊断，不是安全边界。真正的边界仍由
// PathResolver、权限系统和 sandbox 文件系统策略负责。
type Analyzer interface {
	Name() string
	Analyze(input AnalysisInput) ([]Violation, error)
}

// Registry 聚合多个路径分析器。
type Registry struct {
	analyzers []Analyzer
}

// Validator 是执行前路径契约质量门。
type Validator struct {
	registry *Registry
}

// NewRegistry 创建路径分析器注册表。
func NewRegistry(analyzers ...Analyzer) *Registry {
	copied := make([]Analyzer, 0, len(analyzers))
	for _, analyzer := range analyzers {
		if analyzer != nil {
			copied = append(copied, analyzer)
		}
	}
	if len(copied) == 0 {
		copied = append(copied, ShellTextAnalyzer{}, PythonSourceAnalyzer{})
	}
	return &Registry{analyzers: copied}
}

// DefaultRegistry 返回默认路径分析器注册表。
func DefaultRegistry() *Registry {
	return NewRegistry(ShellTextAnalyzer{}, PythonSourceAnalyzer{})
}

// NewValidator 创建路径契约校验器。
func NewValidator(registry *Registry) *Validator {
	if registry == nil {
		registry = DefaultRegistry()
	}
	return &Validator{registry: registry}
}

// Analyze 执行注册表内所有分析器，并按 fragment/location 去重。
func (r *Registry) Analyze(input AnalysisInput) ([]Violation, error) {
	if r == nil {
		r = DefaultRegistry()
	}
	seen := map[string]bool{}
	var out []Violation
	for _, analyzer := range r.analyzers {
		violations, err := analyzer.Analyze(input)
		if err != nil {
			return nil, fmt.Errorf("%s分析失败: %w", analyzer.Name(), err)
		}
		for _, violation := range violations {
			violation = normalizeViolation(analyzer.Name(), violation)
			key := violation.Fragment + "\x00" + violation.Location
			if violation.Fragment == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, violation)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Location == out[j].Location {
			return out[i].Fragment < out[j].Fragment
		}
		return out[i].Location < out[j].Location
	})
	return out, nil
}

// ValidateCommand 在 strict workspace contract 下做执行前路径质量门。
func ValidateCommand(cmd execmodel.Command, opts execcontract.RunOptions) error {
	return NewValidator(nil).ValidateCommand(cmd, opts)
}

// ValidateCommand 在 strict workspace contract 下做执行前路径质量门。
func (v *Validator) ValidateCommand(cmd execmodel.Command, opts execcontract.RunOptions) error {
	if EffectivePathPolicy(opts) != execmodel.PathPolicyStrictWorkspace {
		return nil
	}
	if v == nil {
		v = NewValidator(nil)
	}
	violations, err := v.registry.Analyze(AnalysisInput{Command: cmd, Options: opts})
	if err != nil {
		return execcontract.NewError(execcontract.ErrCodeInvalidInput, err)
	}
	if len(violations) == 0 {
		return nil
	}
	return execcontract.NewError(execcontract.ErrCodeInvalidInput, formatViolationError(violations))
}

// EffectivePathPolicy 推导本次执行应使用的路径策略。
func EffectivePathPolicy(opts execcontract.RunOptions) execmodel.PathPolicy {
	if opts.Workspace.PathPolicy != "" {
		return opts.Workspace.PathPolicy
	}
	if opts.Workspace.Mode == execmodel.WorkspaceModeLocalTask || opts.Workspace.Mode == execmodel.WorkspaceModeSandboxSess {
		return execmodel.PathPolicyStrictWorkspace
	}
	provider := strings.ToLower(strings.TrimSpace(opts.Sandbox.Provider))
	if opts.Sandbox.Mode != execmodel.SandboxDisabled && (strings.Contains(provider, "genesis") || strings.Contains(provider, "remote") || strings.Contains(provider, "docker")) {
		return execmodel.PathPolicyStrictWorkspace
	}
	return execmodel.PathPolicyPermissionOnly
}

// AnalyzeCommand 返回命令字符串中的明显路径契约违规。
func AnalyzeCommand(command string) []Violation {
	violations, _ := DefaultRegistry().Analyze(AnalysisInput{Command: execmodel.Command{Command: command}})
	return violations
}

// ShellTextAnalyzer 对 shell 命令文本做语言无关的路径扫描。
type ShellTextAnalyzer struct{}

func (ShellTextAnalyzer) Name() string { return "shell_text" }

func (ShellTextAnalyzer) Analyze(input AnalysisInput) ([]Violation, error) {
	matches := pathFragmentsInText(input.Command.Command)
	if len(matches) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	violations := make([]Violation, 0, len(matches))
	for _, raw := range matches {
		fragment := trimPathFragment(raw)
		if fragment == "" || seen[fragment] || allowedStrictFragment(fragment) {
			continue
		}
		seen[fragment] = true
		violations = append(violations, violationFor(fragment, "command"))
	}
	return violations, nil
}

// PythonSourceAnalyzer 对能可靠取得源码的 Python 命令做更深一层扫描。
//
// 当前实现关注 Python 字符串字面量中的路径，包括 python -c 和可读取的
// .py 脚本文件。它是语言分析器扩展点的第一版实现，不把任意动态表达式
// 误判为安全；未识别到问题也不代表可以绕过 sandbox/权限边界。
type PythonSourceAnalyzer struct{}

func (PythonSourceAnalyzer) Name() string { return "python_source" }

func (PythonSourceAnalyzer) Analyze(input AnalysisInput) ([]Violation, error) {
	source, location, ok := pythonSourceFromCommand(input.Command)
	if !ok || source == "" {
		return nil, nil
	}
	stringsInSource := pythonStringLiterals(source)
	if len(stringsInSource) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var violations []Violation
	for _, literal := range stringsInSource {
		for _, raw := range pathFragmentsInText(literal) {
			fragment := trimPathFragment(raw)
			if fragment == "" || seen[fragment] || allowedStrictFragment(fragment) {
				continue
			}
			seen[fragment] = true
			v := violationFor(fragment, location)
			v.Analyzer = "python_source"
			violations = append(violations, v)
		}
	}
	return violations, nil
}

func pathFragmentsInText(text string) []string {
	indexes := pathLikePattern.FindAllStringIndex(text, -1)
	if len(indexes) == 0 {
		return nil
	}
	fragments := make([]string, 0, len(indexes))
	for _, index := range indexes {
		start, end := index[0], index[1]
		if start > 0 && text[start-1] == ':' {
			continue
		}
		fragment := text[start:end]
		if start > 0 && len(fragment) > 2 && fragment[1] == ':' && isASCIILetter(text[start-1]) {
			continue
		}
		if strings.HasPrefix(fragment, "/") && start > 0 && !isPathBoundary(text[start-1]) {
			continue
		}
		fragments = append(fragments, fragment)
	}
	return fragments
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isPathBoundary(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '"', '\'', '`', '(', '[', '{', '=', ':', ',':
		return true
	default:
		return false
	}
}

var pathLikePattern = regexp.MustCompile(`(?i)(\$?\{?(?:INPUT_DIR|OUTPUT_DIR|TMPDIR|WORK_DIR|GENESIS_WORKSPACE)\}?[/\\][^\s"'` + "`" + `;|&)]*|%?(?:INPUT_DIR|OUTPUT_DIR|TMPDIR|WORK_DIR|GENESIS_WORKSPACE)%?[/\\][^\s"'` + "`" + `;|&)]*|[a-z]:[/\\][^\s"'` + "`" + `;|&)]*|\\\\[^\s"'` + "`" + `;|&)]*|/[^\s"'` + "`" + `;|&)]*)`)
var windowsAbsPattern = regexp.MustCompile(`^[a-z]:/`)

func normalizeViolation(analyzerName string, violation Violation) Violation {
	if violation.Analyzer == "" {
		violation.Analyzer = analyzerName
	}
	if violation.Severity == "" {
		violation.Severity = SeverityError
	}
	if violation.Location == "" {
		violation.Location = "command"
	}
	return violation
}

func trimPathFragment(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, `"'`)
	raw = strings.TrimRight(raw, `,.:`)
	return raw
}

func allowedStrictFragment(fragment string) bool {
	normalized := strings.ReplaceAll(fragment, `\`, `/`)
	lower := strings.ToLower(normalized)
	switch {
	case strings.HasPrefix(lower, "$input_dir/"),
		strings.HasPrefix(lower, "${input_dir}/"),
		strings.HasPrefix(lower, "%input_dir%/"),
		strings.HasPrefix(lower, "$output_dir/"),
		strings.HasPrefix(lower, "${output_dir}/"),
		strings.HasPrefix(lower, "%output_dir%/"),
		strings.HasPrefix(lower, "$tmpdir/"),
		strings.HasPrefix(lower, "${tmpdir}/"),
		strings.HasPrefix(lower, "%tmpdir%/"),
		strings.HasPrefix(lower, "$work_dir/"),
		strings.HasPrefix(lower, "${work_dir}/"),
		strings.HasPrefix(lower, "%work_dir%/"),
		strings.HasPrefix(lower, "$genesis_workspace/"),
		strings.HasPrefix(lower, "${genesis_workspace}/"),
		strings.HasPrefix(lower, "%genesis_workspace%/"):
		return true
	case lower == "/workspace",
		strings.HasPrefix(lower, "/workspace/"):
		return true
	default:
		return false
	}
}

func violationFor(fragment, location string) Violation {
	normalized := strings.ReplaceAll(fragment, `\`, `/`)
	lower := strings.ToLower(normalized)
	switch {
	case windowsAbsPattern.MatchString(lower),
		strings.HasPrefix(lower, "//"),
		strings.HasPrefix(lower, "/users/"),
		strings.HasPrefix(lower, "/home/"),
		strings.HasPrefix(lower, "/mnt/"),
		strings.HasPrefix(lower, "/volumes/"):
		return Violation{
			Severity: SeverityError,
			Fragment: fragment,
			Location: location,
			Reason:   "sandbox/任务型执行不能直接访问宿主机绝对路径",
			Fix:      "先通过输入 staging 把文件放入 INPUT_DIR，再在代码中读取 INPUT_DIR 下的文件",
		}
	case strings.HasPrefix(lower, "/tmp/"):
		return Violation{
			Severity: SeverityError,
			Fragment: fragment,
			Location: location,
			Reason:   "最终成果或跨步骤状态不能写入 /tmp",
			Fix:      "临时文件使用 TMPDIR，最终成果写入 OUTPUT_DIR，需要跨 job 复用的状态写入 WORK_DIR",
		}
	default:
		return Violation{
			Severity: SeverityError,
			Fragment: fragment,
			Location: location,
			Reason:   "strict workspace contract 下不允许使用非标准绝对路径",
			Fix:      "输入使用 INPUT_DIR，成果使用 OUTPUT_DIR，临时文件使用 TMPDIR，跨 job 状态使用 WORK_DIR",
		}
	}
}

func formatViolationError(violations []Violation) error {
	parts := make([]string, 0, len(violations))
	for _, violation := range violations {
		location := violation.Location
		if location != "" && location != "command" {
			location = location + ": "
		} else {
			location = ""
		}
		parts = append(parts, fmt.Sprintf("%s%s: %s；修复建议：%s", location, violation.Fragment, violation.Reason, violation.Fix))
	}
	return fmt.Errorf("EXECUTION_PATH_CONTRACT_VIOLATION: %s", strings.Join(parts, " | "))
}

func pythonSourceFromCommand(cmd execmodel.Command) (source string, location string, ok bool) {
	fields := splitCommandFields(cmd.Command)
	for i, field := range fields {
		if !isPythonExecutable(field) {
			continue
		}
		if i+2 < len(fields) && fields[i+1] == "-c" {
			return fields[i+2], "python -c", true
		}
		for j := i + 1; j < len(fields); j++ {
			arg := fields[j]
			if strings.HasPrefix(arg, "-") {
				continue
			}
			if !strings.HasSuffix(strings.ToLower(arg), ".py") {
				return "", "", false
			}
			path := resolveScriptPath(cmd.Cwd, arg)
			data, err := os.ReadFile(path)
			if err != nil || len(data) > maxAnalyzedSourceBytes {
				return "", "", false
			}
			return string(data), path, true
		}
	}
	return "", "", false
}

func resolveScriptPath(cwd, arg string) string {
	if filepath.IsAbs(arg) || cwd == "" {
		return arg
	}
	return filepath.Join(cwd, arg)
}

func isPythonExecutable(field string) bool {
	field = strings.Trim(field, `"'`)
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(field, `\`, string(filepath.Separator))))
	return base == "python" || base == "python.exe" || base == "python3" || base == "python3.exe" || base == "py" || base == "py.exe"
}

func splitCommandFields(command string) []string {
	var fields []string
	var b strings.Builder
	var quote rune
	escaped := false
	for _, r := range command {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			b.WriteRune(r)
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if unicode.IsSpace(r) {
			if b.Len() > 0 {
				fields = append(fields, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() > 0 {
		fields = append(fields, b.String())
	}
	return fields
}

func pythonStringLiterals(source string) []string {
	var literals []string
	for i := 0; i < len(source); {
		quote := source[i]
		if quote != '\'' && quote != '"' {
			i++
			continue
		}
		triple := i+2 < len(source) && source[i+1] == quote && source[i+2] == quote
		start := i
		if triple {
			i += 3
		} else {
			i++
		}
		var b strings.Builder
		escaped := false
		for i < len(source) {
			if escaped {
				b.WriteByte(source[i])
				escaped = false
				i++
				continue
			}
			if source[i] == '\\' {
				b.WriteByte(source[i])
				escaped = true
				i++
				continue
			}
			if triple {
				if i+2 < len(source) && source[i] == quote && source[i+1] == quote && source[i+2] == quote {
					i += 3
					literals = append(literals, b.String())
					break
				}
			} else if source[i] == quote {
				i++
				literals = append(literals, b.String())
				break
			}
			b.WriteByte(source[i])
			i++
		}
		if i <= start {
			i = start + 1
		}
	}
	return literals
}
