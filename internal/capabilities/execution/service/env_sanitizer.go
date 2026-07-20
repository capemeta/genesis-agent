package service

import (
	"strings"
)

// EnvSanitizer 负责清理和补充进入沙箱环境的系统环境变量。
type EnvSanitizer struct{}

// NewEnvSanitizer 创建环境变量净化器。
func NewEnvSanitizer() *EnvSanitizer {
	return &EnvSanitizer{}
}

// Sanitize 剥离敏感环境变量，保留基础安全环境变量并自动补全代理环境。
func (s *EnvSanitizer) Sanitize(env map[string]string) map[string]string {
	return s.SanitizeEnv(env)
}

// SanitizeEnv 清理环境变量。
func (s *EnvSanitizer) SanitizeEnv(env map[string]string) map[string]string {
	if env == nil {
		env = make(map[string]string)
	}

	sanitized := make(map[string]string, len(env))

	// 敏感关键字黑名单前缀/后缀模式
	sensitiveKeys := []string{
		"SECRET",
		"PASSWORD",
		"PASSWD",
		"PRIVATE_KEY",
		"TOKEN",
		"CREDENTIAL",
	}

	for k, v := range env {
		upperKey := strings.ToUpper(k)
		isSensitive := false
		for _, sens := range sensitiveKeys {
			if strings.Contains(upperKey, sens) {
				isSensitive = true
				break
			}
		}
		if !isSensitive {
			sanitized[k] = v
		}
	}

	return sanitized
}
