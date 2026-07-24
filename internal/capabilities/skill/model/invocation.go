package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

const (
	RuntimeManifestSchemaV1 = "genesis.skill/v1"
	RuntimeManifestFileName = "genesis.skill.yaml"

	MaxManifestBytes          = 256 * 1024
	MaxInvocationInstructions = 64 * 1024
	MaxInvocations            = 32
	MaxRuntimeProfiles        = 16
	MaxToolsPerInvocation     = 64
	MaxDependenciesPerProfile = 128
	MaxDeliverables           = 16
	MaxInputs                 = 64
)

// RuntimeManifest 是 Genesis 对可移植 Skill 的运行时侧车声明。
// 它属于包控制面，绝不注入模型上下文。
type RuntimeManifest struct {
	Schema          string                    `json:"schema" yaml:"schema"`
	Skill           string                    `json:"skill" yaml:"skill"`
	RuntimeProfiles map[string]RuntimeProfile `json:"runtime_profiles" yaml:"runtime_profiles"`
	Invocations     []InvocationDefinition    `json:"invocations" yaml:"invocations"`
}

// RuntimeProfile 声明逻辑运行依赖和最低沙箱约束，不绑定产品镜像。
type RuntimeProfile struct {
	Sandbox      SandboxRequirement `json:"sandbox" yaml:"sandbox"`
	Dependencies Dependencies       `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
}

type SandboxRequirement struct {
	Required      bool          `json:"required" yaml:"required"`
	ExecutionMode ExecutionMode `json:"execution_mode" yaml:"execution_mode"`
	Backends      []string      `json:"backends,omitempty" yaml:"backends,omitempty"`
}

type ExecutionMode string

const (
	ExecutionModePerCall          ExecutionMode = "per_call"
	ExecutionModeSandboxedSession ExecutionMode = "sandboxed_session"
)

// InvocationDefinition 是一个物理 Skill 的逻辑调用入口。
type InvocationDefinition struct {
	ID             string                  `json:"id" yaml:"id"`
	Handle         string                  `json:"handle" yaml:"handle"`
	Description    string                  `json:"description" yaml:"description"`
	AgentMode      AgentModeSpec           `json:"agent_mode" yaml:"agent_mode"`
	RuntimeProfile string                  `json:"runtime_profile" yaml:"runtime_profile"`
	Request        RequestContract         `json:"request" yaml:"request"`
	Prompt         InvocationPrompt        `json:"prompt" yaml:"prompt"`
	ToolPolicy     ToolPolicy              `json:"tool_policy" yaml:"tool_policy"`
	Requires       []CapabilityRequirement `json:"requires,omitempty" yaml:"requires,omitempty"`
	Result         ResultContract          `json:"result" yaml:"result"`
}

type AgentModeSpec struct {
	Mode         AgentMode `json:"mode" yaml:"mode"`
	TimeoutSec   int       `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
	MaxTurns     int       `json:"max_turns,omitempty" yaml:"max_turns,omitempty"`
	MaxTokens    int64     `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	MaxToolCalls int       `json:"max_tool_calls,omitempty" yaml:"max_tool_calls,omitempty"`
}

func (a *AgentModeSpec) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	var modeStr string
	if err := value.Decode(&modeStr); err == nil {
		modeStr = strings.TrimSpace(modeStr)
		if modeStr == "inline" {
			modeStr = string(AgentModeMain)
		}
		a.Mode = AgentMode(modeStr)
		return nil
	}
	type rawSpec AgentModeSpec
	var raw rawSpec
	if err := value.Decode(&raw); err != nil {
		return err
	}
	if raw.Mode == "inline" {
		raw.Mode = AgentModeMain
	}
	*a = AgentModeSpec(raw)
	return nil
}

type RequestContract struct {
	Task   TaskContract  `json:"task" yaml:"task"`
	Inputs InputContract `json:"inputs" yaml:"inputs"`
}

type TaskContract struct {
	Required bool `json:"required" yaml:"required"`
}

type InputContract struct {
	MinItems         int      `json:"min_items" yaml:"min_items"`
	MaxItems         int      `json:"max_items" yaml:"max_items"`
	Access           string   `json:"access" yaml:"access"`
	AcceptedSuffixes []string `json:"accepted_suffixes,omitempty" yaml:"accepted_suffixes,omitempty"`
	AcceptedMIMEs    []string `json:"accepted_mimes,omitempty" yaml:"accepted_mimes,omitempty"`
}

const InputAccessReadOnly = "read_only"

type InvocationPrompt struct {
	Instructions string        `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	SkillBody    SkillBodyMode `json:"skill_body" yaml:"skill_body"`
}

type SkillBodyMode string

const (
	SkillBodyInclude SkillBodyMode = "include"
	SkillBodyOmit    SkillBodyMode = "omit"
)

type ToolPolicy struct {
	Allow    []string `json:"allow" yaml:"allow"`
	Required []string `json:"required,omitempty" yaml:"required,omitempty"`
}

type ResultContract struct {
	Kind         ResultKind               `json:"kind" yaml:"kind"`
	Deliverables []DeliverableDeclaration `json:"deliverables,omitempty" yaml:"deliverables,omitempty"`
}

type ResultKind string

const (
	ResultKindMessage      ResultKind = "message"
	ResultKindDeliverables ResultKind = "deliverables"
)

type DeliverableDeclaration struct {
	ID               string        `json:"id" yaml:"id"`
	Role             string        `json:"role" yaml:"role"`
	Required         bool          `json:"required" yaml:"required"`
	Cardinality      string        `json:"cardinality" yaml:"cardinality"`
	DesiredName      string        `json:"desired_name,omitempty" yaml:"desired_name,omitempty"`
	AcceptedSuffixes []string      `json:"accepted_suffixes,omitempty" yaml:"accepted_suffixes,omitempty"`
	AcceptedMIMEs    []string      `json:"accepted_mimes,omitempty" yaml:"accepted_mimes,omitempty"`
	DeliveryPolicy   string        `json:"delivery_policy" yaml:"delivery_policy"`
	QA               QADeclaration `json:"qa,omitempty" yaml:"qa,omitempty"`
}

const (
	DeliverableRolePrimary    = "primary"
	DeliverableRoleSupporting = "supporting"
	DeliverableExactlyOne     = "exactly_one"
	DeliverableZeroOrOne      = "zero_or_one"
	DeliverableOneOrMore      = "one_or_more"
	DeliverableZeroOrMore     = "zero_or_more"
	DeliveryPolicyRunOutput   = "run-output"
)

// PackageFileDigest 是包摘要中的单文件事实。
type PackageFileDigest struct {
	Resource ResourceID `json:"resource"`
	SHA256   string     `json:"sha256"`
	Size     int64      `json:"size"`
}

// SkillPackageFile 是 Skill 包在快照存储中的原始文件内容。Resource 始终使用
// "<package_id>/<relative-path>" 命名空间，禁止宿主绝对路径进入运行时契约。
type SkillPackageFile struct {
	Resource ResourceID `json:"resource"`
	Content  []byte     `json:"content"`
}

// SkillPackageSnapshot 固定某次解析使用的完整 Skill 包身份。
type SkillPackageSnapshot struct {
	Authority      Authority           `json:"authority"`
	PackageID      PackageID           `json:"package_id"`
	Version        string              `json:"version,omitempty"`
	Digest         string              `json:"digest"`
	ManifestDigest string              `json:"manifest_digest,omitempty"`
	Files          []PackageFileDigest `json:"files"`
}

// PhysicalSkillDefinition 是 Source 产出的不可变物理 Skill 定义。
type PhysicalSkillDefinition struct {
	Metadata Metadata             `json:"metadata"`
	Manifest *RuntimeManifest     `json:"manifest,omitempty"`
	Snapshot SkillPackageSnapshot `json:"snapshot"`
}

// InvocationMetadata 是模型和 UI 可见的轻量 Catalog 项。
type InvocationMetadata struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	QualifiedName string     `json:"qualified_name"`
	Description   string     `json:"description"`
	PhysicalSkill string     `json:"physical_skill"`
	InvocationID  string     `json:"invocation_id"`
	Scope         Scope      `json:"scope"`
	Authority     Authority  `json:"authority"`
	PackageID     PackageID  `json:"package_id"`
	MainResource  ResourceID `json:"main_resource"`
	Version       string     `json:"version,omitempty"`
	PackageDigest string     `json:"package_digest"`
	Enabled       bool       `json:"enabled"`
	PromptVisible bool       `json:"prompt_visible"`
}

type InvocationCatalog struct {
	Entries  []InvocationMetadata `json:"entries"`
	Errors   []Error              `json:"errors,omitempty"`
	Warnings []string             `json:"warnings,omitempty"`
}

// ResolvedInvocation 是静态声明解析结果，尚未包含当前 Run 的权限和输入事实。
type ResolvedInvocation struct {
	CatalogItem  InvocationMetadata      `json:"catalog_item"`
	Physical     PhysicalSkillDefinition `json:"physical"`
	Definition   InvocationDefinition    `json:"definition"`
	Profile      RuntimeProfile          `json:"profile"`
	SkillBody    string                  `json:"skill_body,omitempty"`
	Instructions string                  `json:"instructions,omitempty"`
}

type EffectiveToolPolicy struct {
	Base     []string `json:"base"`
	Allowed  []string `json:"allowed"`
	Required []string `json:"required"`
}

type EffectiveExecutionPolicy struct {
	SandboxRequired  bool          `json:"sandbox_required"`
	ExecutionMode    ExecutionMode `json:"execution_mode"`
	Backends         []string      `json:"backends,omitempty"`
	SelectedBackend  string        `json:"selected_backend,omitempty"`
	PreferredBackend string        `json:"preferred_backend,omitempty"`
	AllowDegradation bool          `json:"allow_degradation"`
	RequestedBackend string        `json:"requested_backend,omitempty"`
	Degraded         bool          `json:"degraded,omitempty"`
	Warnings         []string      `json:"warnings,omitempty"`
}

type EffectiveCapabilitySnapshot struct {
	VisionMode string    `json:"vision_mode,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
}

type BoundInput struct {
	Ref    workmodel.ResourceRef `json:"ref"`
	Alias  string                `json:"alias,omitempty"`
	SHA256 string                `json:"sha256,omitempty"`
}

// InvocationBinding 是一次调用的不可变运行事实；所有下游只能读取它。
type InvocationBinding struct {
	ID                    string                      `json:"id"`
	TenantID              string                      `json:"tenant_id,omitempty"`
	RunID                 string                      `json:"run_id"`
	ParentRunID           string                      `json:"parent_run_id,omitempty"`
	Package               SkillPackageSnapshot        `json:"package"`
	ManifestSchema        string                      `json:"manifest_schema,omitempty"`
	InvocationID          string                      `json:"invocation_id"`
	Handle                string                      `json:"handle"`
	PhysicalSkill         string                      `json:"physical_skill"`
	AgentMode             AgentModeSpec               `json:"agent_mode"`
	RuntimeProfileID      string                      `json:"runtime_profile_id"`
	RuntimeProfile        RuntimeProfile              `json:"runtime_profile"`
	ToolPolicy            EffectiveToolPolicy         `json:"tool_policy"`
	ExecutionPolicy       EffectiveExecutionPolicy    `json:"execution_policy"`
	Capabilities          EffectiveCapabilitySnapshot `json:"capabilities"`
	Requires              []CapabilityRequirement     `json:"requires,omitempty"`
	Task                  string                      `json:"task,omitempty"`
	Inputs                []BoundInput                `json:"inputs,omitempty"`
	Result                ResultContract              `json:"result"`
	InstructionDigest     string                      `json:"instruction_digest,omitempty"`
	PolicySnapshotVersion string                      `json:"policy_snapshot_version"`
	IdempotencyKey        string                      `json:"idempotency_key"`
	CreatedAt             time.Time                   `json:"created_at"`
}

func (b InvocationBinding) Clone() InvocationBinding {
	out := b
	out.Package.Files = append([]PackageFileDigest(nil), b.Package.Files...)
	out.RuntimeProfile.Dependencies = cloneDependencies(b.RuntimeProfile.Dependencies)
	out.ToolPolicy.Base = append([]string(nil), b.ToolPolicy.Base...)
	out.ToolPolicy.Allowed = append([]string(nil), b.ToolPolicy.Allowed...)
	out.ToolPolicy.Required = append([]string(nil), b.ToolPolicy.Required...)
	out.ExecutionPolicy.Warnings = append([]string(nil), b.ExecutionPolicy.Warnings...)
	out.Requires = append([]CapabilityRequirement(nil), b.Requires...)
	out.Inputs = append([]BoundInput(nil), b.Inputs...)
	out.Result.Deliverables = make([]DeliverableDeclaration, len(b.Result.Deliverables))
	for i, deliverable := range b.Result.Deliverables {
		deliverable.AcceptedSuffixes = append([]string(nil), deliverable.AcceptedSuffixes...)
		deliverable.AcceptedMIMEs = append([]string(nil), deliverable.AcceptedMIMEs...)
		out.Result.Deliverables[i] = deliverable
	}
	return out
}

func cloneDependencies(in Dependencies) Dependencies {
	out := in
	out.Tools = append([]ToolDependency(nil), in.Tools...)
	out.Runtime.Python = append([]RuntimePackage(nil), in.Runtime.Python...)
	out.Runtime.Node = append([]RuntimePackage(nil), in.Runtime.Node...)
	out.Runtime.System = append([]RuntimePackage(nil), in.Runtime.System...)
	out.InstallHints = append([]string(nil), in.InstallHints...)
	return out
}

func StableInvocationID(authority Authority, packageID PackageID, invocationID string) string {
	return authority.String() + ":" + string(packageID) + ":" + strings.TrimSpace(invocationID)
}

func BuildIdempotencyKey(packageDigest, invocationID, task, consumerRun string, inputs []BoundInput) string {
	parts := []string{strings.TrimSpace(packageDigest), strings.TrimSpace(invocationID), normalizeTask(task), strings.TrimSpace(consumerRun)}
	inputKeys := make([]string, 0, len(inputs))
	for _, input := range inputs {
		ref := input.Ref
		inputKeys = append(inputKeys, strings.Join([]string{ref.Authority, ref.Scheme, ref.ID, ref.Version, input.SHA256}, "\x00"))
	}
	sort.Strings(inputKeys)
	parts = append(parts, inputKeys...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "skill-invocation-" + hex.EncodeToString(sum[:16])
}

func normalizeTask(task string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(task)), " ")
}

func ValidateBindingIdentity(binding InvocationBinding) error {
	if strings.TrimSpace(binding.ID) == "" || strings.TrimSpace(binding.RunID) == "" || strings.TrimSpace(binding.Handle) == "" || strings.TrimSpace(binding.InvocationID) == "" || strings.TrimSpace(binding.PhysicalSkill) == "" {
		return fmt.Errorf("invocation binding identity不完整")
	}
	if strings.TrimSpace(string(binding.Package.PackageID)) == "" || strings.TrimSpace(binding.Package.Digest) == "" || strings.TrimSpace(binding.IdempotencyKey) == "" || binding.CreatedAt.IsZero() {
		return fmt.Errorf("invocation binding缺少不可变摘要")
	}
	return nil
}

// IsAncestorSkill 判断给定的 Invocation Metadata（或所属物理技能包）是否匹配祖先链中的任何记录。
func IsAncestorSkill(entry InvocationMetadata, ancestorSet map[string]struct{}) bool {
	if len(ancestorSet) == 0 {
		return false
	}
	keys := []string{
		strings.ToLower(strings.TrimSpace(entry.ID)),
		strings.ToLower(strings.TrimSpace(entry.Name)),
		strings.ToLower(strings.TrimSpace(entry.QualifiedName)),
		strings.ToLower(strings.TrimSpace(entry.PhysicalSkill)),
		strings.ToLower(strings.TrimSpace(string(entry.PackageID))),
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, ok := ancestorSet[key]; ok {
			return true
		}
		for ancestor := range ancestorSet {
			if ancestor == key || strings.Contains(ancestor, ":"+key+":") || strings.HasSuffix(ancestor, ":"+key) || strings.HasPrefix(ancestor, key+":") || strings.Contains(ancestor, ":"+key) || strings.Contains(ancestor, key+":") {
				return true
			}
		}
	}
	return false
}

