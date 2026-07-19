// Package vision 提供视觉能力形态解析（配置真相源，不做模型名猜测）。
package vision

// Mode 是一次 Run 对主会话生效的视觉能力形态。
type Mode string

const (
	ModeDirectInject Mode = "direct_inject" // 主模型可看图
	ModeExpertRoute  Mode = "expert_route"  // 主模型不可看图，走 router.vision
	ModeDegradedText Mode = "degraded_text" // 无视觉能力，诚实降级
)

// ResolveEffectiveVisionMode 根据主模型与 vision 路由模型的 supports_image 求值。
// visionAlias 为空表示未配置 vision 路由。
func ResolveEffectiveVisionMode(mainSupportsImage bool, visionAlias string, visionSupportsImage bool) Mode {
	if mainSupportsImage {
		return ModeDirectInject
	}
	if visionAlias != "" && visionSupportsImage {
		return ModeExpertRoute
	}
	return ModeDegradedText
}
