package repeatguard

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"genesis-agent/internal/domain"
)

// Config Repeat Guard 运行时配置。
type Config struct {
	Enabled                  bool
	MaxIdenticalToolFailures int // 连续失败达该值后，下一次同 key 拦截；0=关闭 L1
	MaxStagnantIterations    int // 连续无进展达该值触发 L2；0=关闭 L2
	MaxConsecutiveFail       int // 任意工具连续失败上限；0=关闭
}

// ConfigFromPolicy 从 RuntimePolicy 解析配置；指针字段 nil 时使用最终版默认值。
func ConfigFromPolicy(p domain.RuntimePolicy) Config {
	cfg := Config{
		Enabled:                  true,
		MaxIdenticalToolFailures: 2,
		MaxStagnantIterations:    5,
		MaxConsecutiveFail:       p.MaxConsecutiveFail,
	}
	if p.RepeatGuardEnabled != nil {
		cfg.Enabled = *p.RepeatGuardEnabled
	}
	if p.MaxIdenticalToolFailures != nil {
		cfg.MaxIdenticalToolFailures = *p.MaxIdenticalToolFailures
	}
	if p.MaxStagnantIterations != nil {
		cfg.MaxStagnantIterations = *p.MaxStagnantIterations
	}
	return cfg
}

// CallFailureState 单个 call_key 的失败状态。
type CallFailureState struct {
	ConsecutiveFailures int
	TotalFailures       int
	LastFailureKind     string
	LastSuggestedAction string
	LastErrorExcerpt    string
	LastResultExcerpt   string
	Blocked             bool
	ToolName            string
	SkillHint           string // 可选：从失败结果解析的 skill
}

// ProgressWindow L2 进展窗口。
type ProgressWindow struct {
	StagnantIterations int
	LastProgressAtIter int
	NoProgressInjected bool
	L1Tightened        bool
}

// IterationSignals 本轮 ReAct 迭代内收集的进展信号。
type IterationSignals struct {
	AnyToolSuccess   bool
	NewFailureKind   bool
	NewArtifact      bool
	UserIntervention bool
	EventCleared     bool
	FinalAnswer      bool
}

// CheckResult 预执行检查结果。
type CheckResult struct {
	Blocked     bool
	Content     string
	Identity    CallIdentity
	FailureKind string // repeated_failure / consecutive_tool_failures
}

// ProgressDecision 迭代末尾 L2 决策。
type ProgressDecision struct {
	HadProgress        bool
	StagnantIterations int
	InjectNoProgress   bool
	NoProgressJSON     string
	PartialComplete    bool
	PartialCompleteMsg string
	L1Tightened        bool
}

// Guard Run 级重复失败 / 无进展防护。
type Guard struct {
	mu sync.Mutex

	cfg   Config
	roots PathRoots

	calls              map[string]*CallFailureState
	seenFailureKinds   map[string]struct{}
	seenArtifacts      map[string]struct{}
	progress           ProgressWindow
	consecutiveAnyFail int
	iter               IterationSignals
}

// New 创建 Guard。
func New(cfg Config) *Guard {
	return &Guard{
		cfg:              cfg,
		calls:            make(map[string]*CallFailureState),
		seenFailureKinds: make(map[string]struct{}),
		seenArtifacts:    make(map[string]struct{}),
	}
}

// SetPathRoots 设置路径改写根（可在 Run 装配后注入）。
func (g *Guard) SetPathRoots(roots PathRoots) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.roots = roots
}

// Reset 清空全部状态（测试/运维）。
func (g *Guard) Reset() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = make(map[string]*CallFailureState)
	g.seenFailureKinds = make(map[string]struct{})
	g.seenArtifacts = make(map[string]struct{})
	g.progress = ProgressWindow{}
	g.consecutiveAnyFail = 0
	g.iter = IterationSignals{}
}

// BeginIteration 每轮 ReAct 开始时重置本轮信号。
func (g *Guard) BeginIteration() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.iter = IterationSignals{}
}

// MarkUserIntervention 标记用户介入（审批通过、新消息等）。
func (g *Guard) MarkUserIntervention() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.iter.UserIntervention = true
}

// Check 执行前检查；Blocked 时不得调用 Execute，且不得 Record 失败入账。
func (g *Guard) Check(toolName, argsJSON string, extraIgnore []string) CheckResult {
	if g == nil || !g.cfg.Enabled {
		return CheckResult{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	id := BuildCallKey(toolName, argsJSON, g.roots, extraIgnore)

	if g.cfg.MaxConsecutiveFail > 0 && g.consecutiveAnyFail >= g.cfg.MaxConsecutiveFail {
		return CheckResult{
			Blocked:     true,
			Content:     buildConsecutiveFailJSON(toolName, g.consecutiveAnyFail, g.cfg.MaxConsecutiveFail),
			Identity:    id,
			FailureKind: "consecutive_tool_failures",
		}
	}

	maxIdent := g.effectiveMaxIdenticalLocked()
	if maxIdent <= 0 {
		return CheckResult{Identity: id}
	}
	st := g.calls[id.CallKey]
	if st != nil && st.ConsecutiveFailures >= maxIdent {
		st.Blocked = true
		return CheckResult{
			Blocked:     true,
			Content:     buildRepeatedFailureJSON(id, st),
			Identity:    id,
			FailureKind: "repeated_failure",
		}
	}
	return CheckResult{Identity: id}
}

// Record 真实 Execute 完成后入账（含业务失败）；拦截路径不得调用。
func (g *Guard) Record(toolName, argsJSON, result string, toolErr error, extraIgnore []string) Outcome {
	if g == nil || !g.cfg.Enabled {
		return ParseOutcome(toolName, result, toolErr)
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	id := BuildCallKey(toolName, argsJSON, g.roots, extraIgnore)
	outcome := ParseOutcome(toolName, result, toolErr)
	if outcome.Skill == "" {
		outcome.Skill = skillFromArgs(argsJSON)
	}

	if outcome.Success {
		delete(g.calls, id.CallKey)
		g.consecutiveAnyFail = 0
		g.iter.AnyToolSuccess = true
		g.noteArtifactsLocked(outcome.Artifacts)
		if strings.EqualFold(strings.TrimSpace(toolName), "install_skill_dependencies") {
			g.clearDependencyMissingLocked(outcome.Skill)
		}
		return outcome
	}

	g.consecutiveAnyFail++
	st := g.calls[id.CallKey]
	if st == nil {
		st = &CallFailureState{ToolName: id.ToolName}
		g.calls[id.CallKey] = st
	}
	st.ConsecutiveFailures++
	st.TotalFailures++
	st.ToolName = id.ToolName
	if outcome.Skill != "" {
		st.SkillHint = outcome.Skill
	}
	if outcome.FailureKind != "" {
		if _, seen := g.seenFailureKinds[outcome.FailureKind]; !seen {
			g.seenFailureKinds[outcome.FailureKind] = struct{}{}
			g.iter.NewFailureKind = true
		}
		st.LastFailureKind = outcome.FailureKind
	}
	if outcome.SuggestedAction != "" {
		st.LastSuggestedAction = outcome.SuggestedAction
	}
	if outcome.ErrorExcerpt != "" {
		st.LastErrorExcerpt = truncateRunes(outcome.ErrorExcerpt, 400)
	}
	st.LastResultExcerpt = outcome.ResultExcerpt
	return outcome
}

// ClearApprovalDenied 用户新批准后清零相关条目（保守：清所有 approval_denied）。
func (g *Guard) ClearApprovalDenied() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	cleared := false
	for k, st := range g.calls {
		if st != nil && st.LastFailureKind == "approval_denied" {
			delete(g.calls, k)
			cleared = true
		}
	}
	if cleared {
		g.iter.EventCleared = true
		g.iter.UserIntervention = true
	}
}

// ClearDependencyMissing 安装成功后清零 dependency_missing 相关条目。
func (g *Guard) ClearDependencyMissing(skill string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.clearDependencyMissingLocked(skill)
}

func (g *Guard) clearDependencyMissingLocked(skill string) {
	skill = strings.TrimSpace(skill)
	cleared := false
	for k, st := range g.calls {
		if st == nil || st.LastFailureKind != "dependency_missing" {
			continue
		}
		// 有 skill 时：优先清匹配 skill 的条目；无 SkillHint 的 run_skill_command 也清（保守）
		if skill != "" {
			if st.SkillHint != "" && !strings.EqualFold(st.SkillHint, skill) {
				continue
			}
		}
		delete(g.calls, k)
		cleared = true
	}
	if cleared {
		g.iter.EventCleared = true
	}
}

// EndIteration 在每轮工具执行结束后评估进展；iteration 为当前 rc.Iteration。
func (g *Guard) EndIteration(iteration int, finalAnswer bool) ProgressDecision {
	if g == nil || !g.cfg.Enabled {
		return ProgressDecision{HadProgress: true}
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	if finalAnswer {
		g.iter.FinalAnswer = true
	}

	had := g.iter.AnyToolSuccess ||
		g.iter.NewFailureKind ||
		g.iter.NewArtifact ||
		g.iter.UserIntervention ||
		g.iter.EventCleared ||
		g.iter.FinalAnswer

	dec := ProgressDecision{HadProgress: had, L1Tightened: g.progress.L1Tightened}

	if g.cfg.MaxStagnantIterations <= 0 {
		if had {
			g.resetProgressLocked(iteration)
		}
		dec.StagnantIterations = g.progress.StagnantIterations
		return dec
	}

	if had {
		g.resetProgressLocked(iteration)
		dec.StagnantIterations = 0
		dec.L1Tightened = false
		return dec
	}

	g.progress.StagnantIterations++
	dec.StagnantIterations = g.progress.StagnantIterations

	if g.progress.StagnantIterations < g.cfg.MaxStagnantIterations {
		return dec
	}

	if g.progress.NoProgressInjected {
		dec.PartialComplete = true
		dec.PartialCompleteMsg = "连续多轮无实质进展，已 partial_complete。请查看此前 no_progress 提示并更换策略或询问用户。"
		return dec
	}

	g.progress.NoProgressInjected = true
	g.progress.L1Tightened = true
	dec.InjectNoProgress = true
	dec.L1Tightened = true
	dec.NoProgressJSON = buildNoProgressJSON(g.progress.StagnantIterations)
	return dec
}

func (g *Guard) resetProgressLocked(iteration int) {
	g.progress.StagnantIterations = 0
	g.progress.LastProgressAtIter = iteration
	g.progress.L1Tightened = false
	g.progress.NoProgressInjected = false
}

func (g *Guard) effectiveMaxIdenticalLocked() int {
	max := g.cfg.MaxIdenticalToolFailures
	if max <= 0 {
		return 0
	}
	if g.progress.L1Tightened && max > 1 {
		return 1
	}
	return max
}

func (g *Guard) noteArtifactsLocked(paths []string) {
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := g.seenArtifacts[p]; !ok {
			g.seenArtifacts[p] = struct{}{}
			g.iter.NewArtifact = true
		}
	}
}

func buildRepeatedFailureJSON(id CallIdentity, st *CallFailureState) string {
	hint := "平台已拦截相同调用。请更换参数/脚本，或先完成 prior.suggested_action，或向用户说明阻塞原因。禁止再次提交相同调用。"
	// 针对参数截断给出专项指引：告知 LLM 改变工具选择，而不是微调内容
	if st.LastFailureKind == "tool_arguments_truncated" {
		hint = "工具参数 JSON 在 LLM 输出中途被截断（content 内容过大超过 max_tokens），已连续失败 " + fmt.Sprintf("%d", st.ConsecutiveFailures) + " 次。" +
			"禁止原样重试。必须改变策略：" +
			"1) 改用 apply_patch 的 Add File 操作写入脚本（+ 前缀行，无需 JSON 转义，截断风险更低）；" +
			"2) 或将内容拆为骨架+多次 append（首次 write_file 写骨架，后续 append=true+expected_hash 追加）；" +
			"3) 或缩减单次写入量。"
	}
	payload := map[string]any{
		"ok":               false,
		"failure_kind":     "repeated_failure",
		"retryable":        false,
		"suggested_action": "change_strategy_or_ask_user",
		"prior": map[string]any{
			"tool":             st.ToolName,
			"call_key_prefix":  id.KeyPrefix,
			"failure_kind":     st.LastFailureKind,
			"count":            st.ConsecutiveFailures,
			"suggested_action": st.LastSuggestedAction,
			"error_excerpt":    st.LastErrorExcerpt,
		},
		"hint": hint,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"ok":false,"failure_kind":"repeated_failure","retryable":false,"suggested_action":"change_strategy_or_ask_user"}`
	}
	return string(data)
}

func buildConsecutiveFailJSON(tool string, count, limit int) string {
	payload := map[string]any{
		"ok":               false,
		"failure_kind":     "consecutive_tool_failures",
		"retryable":        false,
		"suggested_action": "change_strategy_or_ask_user",
		"count":            count,
		"limit":            limit,
		"tool":             tool,
		"hint":             "连续工具失败已达上限。请更换策略或向用户说明阻塞，不要继续盲目重试。",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"ok":false,"failure_kind":"consecutive_tool_failures","retryable":false}`
	}
	return string(data)
}

func buildNoProgressJSON(stagnant int) string {
	payload := map[string]any{
		"ok":                  false,
		"failure_kind":        "no_progress",
		"retryable":           false,
		"suggested_action":    "summarize_blocker_and_ask_user_or_change_approach",
		"stagnant_iterations": stagnant,
		"hint":                "连续多轮无实质进展。请总结阻塞、更换路线或询问用户；不要继续微调无效调用。",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"ok":false,"failure_kind":"no_progress","retryable":false}`
	}
	return string(data)
}

