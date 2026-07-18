package runtime

import (
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	mdLinkPattern     = regexp.MustCompile(`\[[^\]]*\]\(([^)]+\.md)\)`)
	fencedCodePattern = regexp.MustCompile("(?s)```([a-zA-Z0-9_-]*)\\s*\\n(.*?)```")
	inlineCodePattern = regexp.MustCompile("`([^`\n]+)`")
)

// 前置必读链接所在章节（按技能通用结构，不绑定具体技能名）。
var prereqSectionKeywords = []string{"Creating", "Workflow", "Procedure", "Design"}

// 视为「可执行校验命令」的 fenced 语言标签；空标签按行严格判定。
var shellFenceLangs = map[string]struct{}{
	"":           {},
	"bash":       {},
	"sh":         {},
	"shell":      {},
	"zsh":        {},
	"fish":       {},
	"powershell": {},
	"pwsh":       {},
	"cmd":        {},
	"console":    {},
	"terminal":   {},
	"text":       {},
}

var commandHeads = map[string]struct{}{
	"python": {}, "python3": {}, "node": {}, "npm": {}, "npx": {},
	"bash": {}, "sh": {}, "zsh": {}, "pwsh": {}, "powershell": {}, "cmd": {},
	"go": {}, "cargo": {}, "pip": {}, "pip3": {}, "uv": {}, "deno": {},
	"ruby": {}, "perl": {}, "dotnet": {}, "java": {}, "mvn": {}, "gradle": {},
	"make": {}, "cmake": {}, "docker": {}, "kubectl": {}, "curl": {}, "wget": {},
	"git": {}, "rscript": {}, "php": {}, "lua": {}, "swift": {},
}

// SkillFollowState 跟踪本 Run 已加载技能正文中的必读引用与 QA 完成情况（软门禁）。
type SkillFollowState struct {
	mu             sync.Mutex
	bodies         []string
	requiredReads  map[string]struct{} // 全文 .md 链接（备用）
	creatingReads  map[string]struct{} // 前置章节内链接：产出前必读
	readSet        map[string]struct{}
	qaCommands     []string            // 从 QA 章节抽出的校验命令
	qaMatched      map[string]struct{} // 已成功匹配的声明命令
	requiresQA     bool
	qaDone         bool
	qaEnvFailures  map[string]struct{} // 确定性环境失败的 QA 命令；同命令不再执行
	qaFailedActual map[string]string   // normalize 后的实际失败命令，用于终态审计，不用模板命令冒充
}

// NewSkillFollowState 创建空跟踪状态。
func NewSkillFollowState() *SkillFollowState {
	return &SkillFollowState{
		requiredReads:  make(map[string]struct{}),
		creatingReads:  make(map[string]struct{}),
		readSet:        make(map[string]struct{}),
		qaMatched:      make(map[string]struct{}),
		qaEnvFailures:  make(map[string]struct{}),
		qaFailedActual: make(map[string]string),
	}
}

// NoteQAEnvironmentFailure 记录不可通过原样重试解决的 QA 环境失败。
func (s *SkillFollowState) NoteQAEnvironmentFailure(command, failureKind string) {
	if s == nil || !isTerminalQAEnvironmentFailure(failureKind) {
		return
	}
	key := normalizeCommand(command)
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.qaEnvFailures[key] = struct{}{}
	s.qaFailedActual[key] = strings.TrimSpace(command)
}

// ShouldBlockQA 表示命令已经确定性失败，或本 Run 的 QA 环境失败预算已耗尽。
func (s *SkillFollowState) ShouldBlockQA(command string) bool {
	if s == nil {
		return false
	}
	key := normalizeCommand(command)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, failed := s.qaEnvFailures[key]; failed {
		return true
	}
	return len(s.qaEnvFailures) >= 2 && s.matchesQACommandLocked(command)
}

func isTerminalQAEnvironmentFailure(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "dependency_missing", "sandbox_unavailable", "unsupported_environment", "qa_unavailable":
		return true
	default:
		return false
	}
}

// RegisterInjection 登记一次技能注入正文，解析其中的 .md 链接与 QA 要求。
func (s *SkillFollowState) RegisterInjection(content string) {
	if s == nil {
		return
	}
	content = normalizeMarkdownNewlines(strings.TrimSpace(content))
	if content == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies = append(s.bodies, content)
	for _, m := range mdLinkPattern.FindAllStringSubmatch(content, -1) {
		rel := normalizeSkillRel(m[1])
		if rel != "" {
			s.requiredReads[rel] = struct{}{}
		}
	}
	for _, kw := range prereqSectionKeywords {
		for _, rel := range extractSectionMarkdownLinks(content, kw) {
			s.creatingReads[rel] = struct{}{}
		}
	}
	qaBody := extractSectionBody(content, "QA")
	if qaBody == "" {
		qaBody = extractSectionBody(content, "Content QA")
	}
	cmds := extractCommandsFromSection(qaBody)
	if len(cmds) > 0 {
		s.requiresQA = true
		s.qaCommands = mergeUniqueCommands(s.qaCommands, cmds)
	} else if sectionHeadingExists(content, "QA") || strings.Contains(content, "QA (Required)") {
		// 有 QA 章节但抽不出命令：仍提醒，完成态只能靠显式 MarkQADone（软门禁可接受）
		s.requiresQA = true
	}
}

// MarkResourceRead 记录已读技能资源（相对路径或带 package 前缀均可）。
func (s *SkillFollowState) MarkResourceRead(resource string) {
	if s == nil {
		return
	}
	rel := normalizeSkillRel(resource)
	if rel == "" {
		rel = strings.ReplaceAll(strings.TrimSpace(resource), `\`, `/`)
		rel = path.Clean(rel)
		if rel == "." || rel == "" {
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readSet[rel] = struct{}{}
	if base := path.Base(rel); base != "" && base != rel {
		s.readSet[base] = struct{}{}
	}
}

// NoteExecutedCommand 根据实际执行的命令判断是否完成技能声明的 QA。
// success=false 时不记完成（失败的校验不算做过 QA）。
// 含管道的声明命令视为可选（如 grep 无匹配常 exit 1）；其余声明命令须全部成功匹配才算 QADone。
func (s *SkillFollowState) NoteExecutedCommand(command string, success bool) {
	if s == nil || !success {
		return
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, qa := range s.qaCommands {
		if commandMatchesQA(normalizeCommand(command), normalizeCommand(qa)) {
			s.qaMatched[qa] = struct{}{}
		}
	}
	s.qaDone = s.allRequiredQAMatchedLocked()
}

// MarkQADone 标记本 Run 已完成技能声明的 QA（显式调用；通常由 NoteExecutedCommand 自动完成）。
func (s *SkillFollowState) MarkQADone() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.qaDone = true
}

// PendingQACommands 返回尚未成功执行的必做 QA 命令（不含管道可选命令）。
func (s *SkillFollowState) PendingQACommands() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0)
	for _, qa := range s.qaCommands {
		if isOptionalQACommand(qa) {
			continue
		}
		if _, ok := s.qaMatched[qa]; ok {
			continue
		}
		out = append(out, qa)
	}
	return out
}

// FailedQACommands 返回发生确定性环境失败的实际执行命令，而不是 Skill 中的占位模板。
func (s *SkillFollowState) FailedQACommands() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.qaFailedActual))
	for _, command := range s.qaFailedActual {
		if command != "" {
			out = append(out, command)
		}
	}
	sort.Strings(out)
	return out
}

func (s *SkillFollowState) allRequiredQAMatchedLocked() bool {
	required := 0
	for _, qa := range s.qaCommands {
		if isOptionalQACommand(qa) {
			continue
		}
		required++
		if _, ok := s.qaMatched[qa]; !ok {
			return false
		}
	}
	// 无抽出必做命令时：任意一次匹配即完成（兼容旧行为）
	if required == 0 {
		return len(s.qaMatched) > 0
	}
	return true
}

func isOptionalQACommand(cmd string) bool {
	return strings.Contains(cmd, "|")
}

// UnreadCreatingRequired 返回前置章节内尚未读取的 .md。
func (s *SkillFollowState) UnreadCreatingRequired() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0)
	for rel := range s.creatingReads {
		if s.isReadLocked(rel) {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// UnreadRequired 返回全文尚未读取的必读 .md（调试/扩展用）。
func (s *SkillFollowState) UnreadRequired() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0)
	for rel := range s.requiredReads {
		if s.isReadLocked(rel) {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// RequiresQA 是否要求执行技能声明的 QA。
func (s *SkillFollowState) RequiresQA() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requiresQA
}

// QADone 是否已跑过与技能 QA 匹配的命令。
func (s *SkillFollowState) QADone() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.qaDone
}

// QACommands 返回已解析的 QA 命令副本。
func (s *SkillFollowState) QACommands() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.qaCommands))
	copy(out, s.qaCommands)
	return out
}

// IsQACommand 判断命令是否匹配技能 QA 章节中的校验命令。
func (s *SkillFollowState) IsQACommand(command string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.matchesQACommandLocked(command)
}

func (s *SkillFollowState) matchesQACommandLocked(command string) bool {
	norm := normalizeCommand(command)
	if norm == "" {
		return false
	}
	for _, qa := range s.qaCommands {
		q := normalizeCommand(qa)
		if q == "" {
			continue
		}
		if commandMatchesQA(norm, q) {
			return true
		}
	}
	return false
}

func (s *SkillFollowState) isReadLocked(rel string) bool {
	if _, ok := s.readSet[rel]; ok {
		return true
	}
	base := path.Base(rel)
	if _, ok := s.readSet[base]; ok {
		return true
	}
	for read := range s.readSet {
		if strings.HasSuffix(read, "/"+rel) || strings.HasSuffix(read, "\\"+rel) || path.Base(read) == base {
			return true
		}
	}
	return false
}

func extractSectionMarkdownLinks(content, sectionKeyword string) []string {
	body := extractSectionBody(content, sectionKeyword)
	out := make([]string, 0)
	seen := map[string]struct{}{}
	for _, m := range mdLinkPattern.FindAllStringSubmatch(body, -1) {
		rel := normalizeSkillRel(m[1])
		if rel == "" {
			continue
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		out = append(out, rel)
	}
	return out
}

func extractSectionBody(content, sectionKeyword string) string {
	lines := strings.Split(content, "\n")
	inSection := false
	var body strings.Builder
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "## ") {
			heading := strings.TrimSpace(strings.TrimPrefix(trim, "## "))
			if inSection {
				break
			}
			if headingMatches(heading, sectionKeyword) {
				inSection = true
				continue
			}
		}
		if inSection {
			body.WriteString(line)
			body.WriteByte('\n')
		}
	}
	return body.String()
}

func sectionHeadingExists(content, sectionKeyword string) bool {
	for _, line := range strings.Split(content, "\n") {
		trim := strings.TrimSpace(line)
		if !strings.HasPrefix(trim, "## ") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimPrefix(trim, "## "))
		if headingMatches(heading, sectionKeyword) {
			return true
		}
	}
	return false
}

// headingMatches 以标题前缀匹配章节名，避免 "Editing Workflow" 误命中 "Workflow"。
func headingMatches(heading, keyword string) bool {
	h := strings.TrimSpace(heading)
	k := strings.TrimSpace(keyword)
	if h == "" || k == "" {
		return false
	}
	if strings.EqualFold(h, k) {
		return true
	}
	if len(h) <= len(k) {
		return false
	}
	if !strings.EqualFold(h[:len(k)], k) {
		return false
	}
	c := h[len(k)]
	return c == ' ' || c == '(' || c == ':' || c == '-' || c == '/'
}

func extractCommandsFromSection(section string) []string {
	section = normalizeMarkdownNewlines(section)
	if strings.TrimSpace(section) == "" {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	add := func(raw string) {
		for _, part := range splitShellChain(raw) {
			cmd := strings.TrimSpace(part)
			cmd = strings.TrimLeft(cmd, "$ ")
			cmd = strings.TrimSpace(cmd)
			if cmd == "" || strings.HasPrefix(cmd, "#") {
				continue
			}
			if !looksLikeCommand(cmd) {
				continue
			}
			if _, ok := seen[cmd]; ok {
				continue
			}
			seen[cmd] = struct{}{}
			out = append(out, cmd)
		}
	}
	for _, m := range fencedCodePattern.FindAllStringSubmatch(section, -1) {
		lang := strings.ToLower(strings.TrimSpace(m[1]))
		if _, ok := shellFenceLangs[lang]; !ok {
			continue // 跳过 prompt/json/markdown 等非 shell fence
		}
		for _, line := range strings.Split(m[2], "\n") {
			add(line)
		}
	}
	// 行内 code：仅在去掉 fenced 块后、且通过严格命令判定时收录
	stripped := fencedCodePattern.ReplaceAllString(section, "\n")
	for _, m := range inlineCodePattern.FindAllStringSubmatch(stripped, -1) {
		add(m[1])
	}
	return out
}

func looksLikeCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	// 拒绝明显散文 / markdown
	if strings.HasPrefix(cmd, "**") || strings.HasPrefix(cmd, "- ") || strings.HasPrefix(cmd, "* ") ||
		strings.HasPrefix(cmd, "###") || strings.HasPrefix(cmd, "Look for") ||
		strings.Contains(cmd, "://") && !strings.Contains(cmd, " ") {
		return false
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return false
	}
	head := strings.ToLower(strings.Trim(fields[0], `"'`))
	head = strings.TrimSuffix(head, ".exe")
	if _, ok := commandHeads[head]; ok {
		// 单独解释器名不够；至少还要有参数或子命令
		return len(fields) >= 2
	}
	if strings.HasPrefix(head, "./") || strings.HasPrefix(head, "../") || strings.HasPrefix(head, "/") ||
		strings.HasPrefix(head, ".\\") || strings.Contains(head, "/") || strings.Contains(head, `\`) {
		return true
	}
	lower := head
	if strings.HasSuffix(lower, ".py") || strings.HasSuffix(lower, ".js") || strings.HasSuffix(lower, ".mjs") ||
		strings.HasSuffix(lower, ".ts") || strings.HasSuffix(lower, ".sh") || strings.HasSuffix(lower, ".ps1") ||
		strings.HasSuffix(lower, ".bash") || strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat") {
		return true
	}
	return false
}

func splitShellChain(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// 管道/与或链：整句保留为一条（匹配时用主命令头），同时再拆出第一段便于匹配
	return []string{raw}
}

func mergeUniqueCommands(dst, src []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(dst)+len(src))
	for _, c := range dst {
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	for _, c := range src {
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

func normalizeMarkdownNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func normalizeCommand(cmd string) string {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	cmd = strings.ReplaceAll(cmd, `\`, `/`)
	fields := strings.Fields(cmd)
	return strings.Join(fields, " ")
}

// commandMatchesQA：执行命令与技能声明的 QA 命令对齐（允许产物路径不同）。
func commandMatchesQA(executed, declared string) bool {
	if executed == declared {
		return true
	}
	// 声明命令是执行命令的子串（常见：多了路径无关前缀时不走这条）
	if len(declared) >= 8 && strings.Contains(executed, declared) {
		return true
	}
	exHead, exRest := splitHeadRest(executed)
	decHead, decRest := splitHeadRest(declared)
	if exHead == "" || exHead != decHead {
		return false
	}
	exToks := significantTokens(exRest)
	decToks := significantTokens(decRest)
	if len(decToks) == 0 {
		return len(exToks) == 0
	}
	// 声明中的非路径 token 必须出现在执行命令中；路径类 token 只要求「同类后缀」或跳过具体文件名
	exSet := map[string]struct{}{}
	for _, t := range exToks {
		exSet[t] = struct{}{}
		exSet[path.Base(t)] = struct{}{}
	}
	needed := 0
	hit := 0
	for _, t := range decToks {
		if isPathLikeToken(t) {
			// 路径参数：只要执行侧也有同后缀产物即可
			if hasPathWithSuffix(exToks, pathExt(t)) {
				hit++
			}
			needed++
			continue
		}
		needed++
		if _, ok := exSet[t]; ok {
			hit++
			continue
		}
		if _, ok := exSet[path.Base(t)]; ok {
			hit++
		}
	}
	if needed == 0 {
		return false
	}
	// 全部非可选 token 命中，或至少命中 2 个关键 token（覆盖 python -m markitdown <file>）
	if hit == needed {
		return true
	}
	return hit >= 2 && hit*2 >= needed
}

func splitHeadRest(cmd string) (head string, rest string) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return "", ""
	}
	head = strings.Trim(fields[0], `"'`)
	head = strings.TrimSuffix(head, ".exe")
	if len(fields) == 1 {
		return head, ""
	}
	return head, strings.Join(fields[1:], " ")
}

func isPathLikeToken(t string) bool {
	t = strings.Trim(t, `"'`)
	if strings.Contains(t, "/") || strings.Contains(t, `\`) {
		return true
	}
	ext := pathExt(t)
	return ext != "" && len(ext) <= 5
}

func pathExt(t string) string {
	t = strings.Trim(t, `"'`)
	base := path.Base(strings.ReplaceAll(t, `\`, `/`))
	i := strings.LastIndex(base, ".")
	if i <= 0 || i == len(base)-1 {
		return ""
	}
	return strings.ToLower(base[i:])
}

func hasPathWithSuffix(toks []string, ext string) bool {
	if ext == "" {
		return true
	}
	for _, t := range toks {
		if pathExt(t) == ext {
			return true
		}
	}
	return false
}

func significantTokens(cmd string) []string {
	skip := map[string]struct{}{
		"-m": {}, "-c": {}, "--": {}, "and": {}, "or": {}, "then": {},
		"|": {}, "||": {}, "&&": {}, ">": {}, ">>": {}, "<": {},
	}
	out := make([]string, 0)
	for _, t := range strings.Fields(cmd) {
		t = strings.Trim(t, `"'`)
		if t == "" {
			continue
		}
		if _, ok := skip[t]; ok {
			continue
		}
		if strings.HasPrefix(t, "-") && len(t) <= 3 {
			continue
		}
		out = append(out, strings.ToLower(t))
	}
	return out
}

func normalizeSkillRel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.Split(raw, "#")[0]
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, `\`, `/`)
	raw = strings.TrimPrefix(raw, "./")
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(raw), ".md") {
		return ""
	}
	return path.Clean(raw)
}
