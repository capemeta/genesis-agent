package context

import (
	"context"
	"fmt"
	"math"
)

// BudgetWeights 弹性预算的目标比例权重
type BudgetWeights struct {
	History float64 `json:"history"`
	Summary float64 `json:"summary"`
	LTM     float64 `json:"ltm"`
}

// BudgetClamp 弹性预算分配的比率上下限区间 [Min, Max] (占可分配输入预算的百分比)
type BudgetClamp struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// ContextProfile 场景化预算配置 Profile
type ContextProfile struct {
	Weights BudgetWeights          `json:"weights"`
	Clamps  map[string]BudgetClamp `json:"clamps"`
}

// DistributableBudget 最终计算出的各弹性段 Token 额度结果
type DistributableBudget struct {
	History int
	Summary int
	LTM     int
}

// ContextBudgetPlanner 上下文窗口预算管理器
type ContextBudgetPlanner struct {
	profiles map[string]ContextProfile
}

// NewContextBudgetPlanner 初始化预算管理器并预置默认 Profile
func NewContextBudgetPlanner(customProfiles map[string]ContextProfile) *ContextBudgetPlanner {
	// 默认的场景配置
	profiles := map[string]ContextProfile{
		"default": {
			Weights: BudgetWeights{History: 0.60, Summary: 0.15, LTM: 0.25},
			Clamps: map[string]BudgetClamp{
				"history": {Min: 0.20, Max: 0.90},
				"summary": {Min: 0.00, Max: 0.30},
				"ltm":     {Min: 0.00, Max: 0.40},
			},
		},
		"rag": {
			Weights: BudgetWeights{History: 0.30, Summary: 0.10, LTM: 0.60},
			Clamps: map[string]BudgetClamp{
				"history": {Min: 0.15, Max: 0.60},
				"summary": {Min: 0.00, Max: 0.20},
				"ltm":     {Min: 0.20, Max: 0.80},
			},
		},
		"chat": {
			Weights: BudgetWeights{History: 0.75, Summary: 0.15, LTM: 0.10},
			Clamps: map[string]BudgetClamp{
				"history": {Min: 0.40, Max: 0.95},
				"summary": {Min: 0.00, Max: 0.30},
				"ltm":     {Min: 0.00, Max: 0.20},
			},
		},
		"coding": {
			Weights: BudgetWeights{History: 0.55, Summary: 0.20, LTM: 0.25},
			Clamps: map[string]BudgetClamp{
				"history": {Min: 0.20, Max: 0.85},
				"summary": {Min: 0.05, Max: 0.35},
				"ltm":     {Min: 0.00, Max: 0.40},
			},
		},
	}

	// 融合用户自定义的 profiles
	for k, v := range customProfiles {
		profiles[k] = v
	}

	return &ContextBudgetPlanner{profiles: profiles}
}

// ResolveProfile 解析获取命名策略 Profile；不存在则回退至 default
func (p *ContextBudgetPlanner) ResolveProfile(strategy string) ContextProfile {
	profile, ok := p.profiles[strategy]
	if !ok {
		return p.profiles["default"]
	}
	return profile
}

// ConvertProfileConfig 辅助转换外部配置结构至内核 ContextProfile，避免反向依赖 platform/config 包。
func ConvertProfileConfig(weights map[string]float64, clamp map[string][]float64) ContextProfile {
	profile := ContextProfile{
		Weights: BudgetWeights{
			History: weights["history"],
			Summary: weights["summary"],
			LTM:     weights["ltm"],
		},
		Clamps: make(map[string]BudgetClamp),
	}

	for k, v := range clamp {
		if len(v) >= 2 {
			profile.Clamps[k] = BudgetClamp{Min: v[0], Max: v[1]}
		}
	}
	return profile
}

// PlanOptions 计算预算所需的当前会话和模型窗口状态
type PlanOptions struct {
	ContextWindow         int     // 模型总上下文大小（例如 128,000）
	EffectiveContextRatio float64 // 有效比例（默认 0.92）
	MaxTokens             int     // 模型单次输出限制（用于预留输出）
	OutputReserveTokens   int     // 显式配置的输出预留 Token（为 0 时使用 MaxTokens）
	Strategy              string  // 场景化 Profile 名称 (rag/chat/coding/default)

	// 实际各输入内容实测 Token 长度，用以进行 reflow 预算结余重分配
	StableSystemTokens int // 刚性：system prompt Persona 稳定段长度
	ToolsSchemaTokens  int // 刚性：工具 definitions 长度
	CurrentUserTokens  int // 刚性：当前单轮用户输入 + reminder 长度

	// 弹性内容实际有的数据量大小（用于 reflow，若内容很少不需要分那么多预算）
	ActualSummaryTokens int // 实际摘要大小
	ActualLTMTokens     int // 实际检索出来的 LTM 大小
}

const (
	MinOutputReserve = 1024
	MaxOutputReserve = 8192
)

// Plan 计算弹性段预算分配
func (p *ContextBudgetPlanner) Plan(ctx context.Context, opt PlanOptions) (DistributableBudget, int, error) {
	// 1. 可用输入总预算计算
	ratio := opt.EffectiveContextRatio
	if ratio <= 0 || ratio > 1 {
		ratio = 0.92
	}
	usable := int(float64(opt.ContextWindow) * ratio)

	reserve := opt.OutputReserveTokens
	if reserve <= 0 {
		reserve = opt.MaxTokens
	}
	// clamp 限制 reserve 在安全合理区间
	if reserve < MinOutputReserve {
		reserve = MinOutputReserve
	} else if reserve > MaxOutputReserve {
		reserve = MaxOutputReserve
	}

	inputBudget := usable - reserve

	// 2. 先扣除刚性段
	rigid := opt.StableSystemTokens + opt.ToolsSchemaTokens + opt.CurrentUserTokens
	remaining := inputBudget - rigid
	if remaining < 0 {
		// 刚性段直接放不下，返回错误交由上层降级
		return DistributableBudget{}, inputBudget, fmt.Errorf("insufficient context budget for rigid inputs: required=%d, budget=%d", rigid, inputBudget)
	}

	// 3. 弹性段分配（clamp + reflow 算法）
	safetyBuffer := int(float64(remaining) * 0.05) // 5% 安全缓存
	distributable := remaining - safetyBuffer
	if distributable <= 0 {
		return DistributableBudget{History: 0, Summary: 0, LTM: 0}, inputBudget, nil
	}

	profile := p.ResolveProfile(opt.Strategy)

	// 执行 clamp + reflow 迭代
	historyAlloc, summaryAlloc, ltmAlloc := reflowAlloc(
		distributable,
		profile,
		opt.ActualSummaryTokens,
		opt.ActualLTMTokens,
	)

	return DistributableBudget{
		History: historyAlloc,
		Summary: summaryAlloc,
		LTM:     ltmAlloc,
	}, inputBudget, nil
}

// reflowAlloc 实现 clamp + reflow 迭代收敛算法
func reflowAlloc(total int, profile ContextProfile, actualSummary, actualLTM int) (history, summary, ltm int) {
	// 定义段分配状态
	type segment struct {
		name       string
		weight     float64
		minRatio   float64
		maxRatio   float64
		actualSize int // 实际内容大小，-1表示无限制（如 history 不设实际大小上限）
		allocated  int
		saturated  bool // 是否已经饱和（达到上限、下限或者实际内容上限）
	}

	hClamp := profile.Clamps["history"]
	sClamp := profile.Clamps["summary"]
	lClamp := profile.Clamps["ltm"]

	segs := []*segment{
		{name: "history", weight: profile.Weights.History, minRatio: hClamp.Min, maxRatio: hClamp.Max, actualSize: -1},
		{name: "summary", weight: profile.Weights.Summary, minRatio: sClamp.Min, maxRatio: sClamp.Max, actualSize: actualSummary},
		{name: "ltm", weight: profile.Weights.LTM, minRatio: lClamp.Min, maxRatio: lClamp.Max, actualSize: actualLTM},
	}

	remainingBudget := total
	for {
		// 计算当前未饱和段的总权重
		var activeWeight float64
		var activeCount int
		for _, seg := range segs {
			if !seg.saturated {
				activeWeight += seg.weight
				activeCount++
			}
		}

		// 没有未饱和段了，或者剩余预算为0，退出
		if activeCount == 0 || remainingBudget <= 0 {
			break
		}

		// 按比例预分配剩余预算
		anyChange := false
		for _, seg := range segs {
			if seg.saturated {
				continue
			}

			var share int
			if activeWeight > 0 {
				share = int(math.Floor(float64(remainingBudget) * (seg.weight / activeWeight)))
			} else {
				share = remainingBudget / activeCount
			}

			candidate := seg.allocated + share
			minVal := int(float64(total) * seg.minRatio)
			maxVal := int(float64(total) * seg.maxRatio)

			// 限制区间
			finalAlloc := candidate
			if finalAlloc < minVal {
				finalAlloc = minVal
			}
			if finalAlloc > maxVal {
				finalAlloc = maxVal
				seg.saturated = true
			}

			// 如果是实际内容受限的段（如 summary/ltm），分配额不需要超过实际大小
			if seg.actualSize >= 0 && finalAlloc > seg.actualSize {
				// 如果实际大小小于最小值，依然要保留最小值作为防饥饿占位（或者实际为0时取0，这里取 max(actual, min)）
				effectiveLimit := seg.actualSize
				if effectiveLimit < minVal {
					effectiveLimit = minVal
				}
				if finalAlloc > effectiveLimit {
					finalAlloc = effectiveLimit
					seg.saturated = true
				}
			}

			diff := finalAlloc - seg.allocated
			if diff > 0 {
				seg.allocated = finalAlloc
				remainingBudget -= diff
				anyChange = true
			}
		}

		// 如果在本轮分配中没有段发生预算增加，说明已达到各自区间上限，必须收敛退出
		if !anyChange {
			break
		}
	}

	// 最后的结余（因为 Floor 舍入或所有段全饱和但仍有盈余），强力补偿给 history 段
	if remainingBudget > 0 {
		for _, seg := range segs {
			if seg.name == "history" {
				maxVal := int(float64(total) * seg.maxRatio)
				if seg.allocated+remainingBudget <= maxVal {
					seg.allocated += remainingBudget
				} else {
					seg.allocated = maxVal
				}
				break
			}
		}
	}

	var histVal, summVal, ltmVal int
	for _, seg := range segs {
		switch seg.name {
		case "history":
			histVal = seg.allocated
		case "summary":
			summVal = seg.allocated
		case "ltm":
			ltmVal = seg.allocated
		}
	}

	return histVal, summVal, ltmVal
}
