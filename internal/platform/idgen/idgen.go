package idgen

import "github.com/google/uuid"

// Generator 定义 ID 生成器接口
type Generator interface {
	Generate() string
}

// UUIDGenerator 基于 UUID 的生成器实现
type UUIDGenerator struct{}

// NewUUIDGenerator 创建一个 UUID 生成器
func NewUUIDGenerator() *UUIDGenerator {
	return &UUIDGenerator{}
}

// Generate 生成一个新的 UUID 字符串
func (g *UUIDGenerator) Generate() string {
	return uuid.New().String()
}
