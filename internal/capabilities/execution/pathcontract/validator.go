// Package pathcontract 校验代码执行的路径契约。
package pathcontract

import (
	"fmt"
	"sort"
	"strings"

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
		copied = append(copied, defaultAnalyzers()...)
	}
	return &Registry{analyzers: copied}
}

// DefaultRegistry 返回默认路径分析器注册表。
func DefaultRegistry() *Registry {
	return NewRegistry(defaultAnalyzers()...)
}

func defaultAnalyzers() []Analyzer {
	return []Analyzer{
		ShellTextAnalyzer{},
		PythonSourceAnalyzer{},
		JavaScriptSourceAnalyzer{},
		GoSourceAnalyzer{},
		JavaSourceAnalyzer{},
		PowerShellSourceAnalyzer{},
		ShellScriptAnalyzer{},
		SkillManifestAnalyzer{},
	}
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
