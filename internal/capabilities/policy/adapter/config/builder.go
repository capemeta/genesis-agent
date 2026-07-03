// Package config 将平台配置适配为 policy evaluator。
package config

import (
	policycontract "genesis-agent/internal/capabilities/policy/contract"
	policyfs "genesis-agent/internal/capabilities/policy/matcher/filesystem"
	policyservice "genesis-agent/internal/capabilities/policy/service"
	platformconfig "genesis-agent/internal/platform/config"
)

// BuildEvaluator 根据平台 policy 配置构建统一策略评估器。
func BuildEvaluator(cfg platformconfig.PolicyConfig) policycontract.Evaluator {
	return policyservice.NewEvaluator(
		cfg.Defaults,
		policyfs.New(cfg.Defaults, cfg.Files),
	)
}
