// Package app 应用服务层：定义业务用例接口，协调领域对象完成业务操作
// 是连接接入层（CLI / HTTP API / Desktop）与领域层的桥梁
//
// 设计原则：
//   - 接入层只依赖本包定义的 AgentService 接口
//   - 不向接入层暴露 engine / memory / tool 等领域包的具体类型
//   - 所有用例均通过接口约定，便于 mock 测试
package app

import (
	"context"
	"time"

	connection "genesis-agent/internal/capabilities/connection/contract"
	credential "genesis-agent/internal/capabilities/credential/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	memory "genesis-agent/internal/capabilities/memory/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/config"
	"genesis-agent/internal/runtime"
	"genesis-agent/internal/runtime/progress"
)

// AgentService 应用服务接口
// 定义所有接入层（CLI、HTTP API、Desktop App）可使用的 Agent 能力
// 面向接口编程：各接入层依赖此接口，不依赖具体实现，便于替换和测试
type AgentService interface {
	// RunOnce 同步执行一次 Agent 推理
	// 适合 HTTP API 请求、CLI run 命令等需要等待结果的场景
	RunOnce(ctx context.Context, req RunRequest) (*RunResult, error)

	// ClearSession 清空指定会话的短期记忆历史
	// 适合 TUI /clear 命令、HTTP API 重置会话接口
	ClearSession(ctx context.Context, sessionID string) error

	// NewSession 创建新的对话会话（每次独立对话前调用）
	NewSession() *domain.Session

	// ListTools 返回所有已注册工具的元信息列表
	ListTools() []*tool.Info

	// Config 返回当前应用配置（只读，用于展示）
	Config() *config.Config

	// DefaultAgent 返回默认 Agent 配置
	DefaultAgent() *domain.Agent

	// Credentials 返回密钥管理服务。
	Credentials() credential.Service

	// Connections 返回业务连接管理服务。
	Connections() connection.Service
}

// RunRequest RunOnce 的请求参数
type RunRequest struct {
	SessionID  string        // 会话 ID（跨 Run 保持对话历史）
	TenantID   string        // 租户 ID（隔离租户数据）
	Input      string        // 用户输入内容
	Agent      *domain.Agent // 可选：指定 Agent 配置；nil 时使用 DefaultAgent
	Sandbox    *execmodel.SandboxProfile
	OnProgress progress.Sink // 可选：接收结构化运行进度事件
}

// RunResult RunOnce 的执行结果
type RunResult struct {
	Run     *domain.Run   // 完整 Run 记录（含所有 Step）
	Elapsed time.Duration // 端到端耗时（从发起到收到最终回答）
}

// agentServiceImpl AgentService 的生产实现
// 通过构造函数注入所有依赖，保证可测试性和替换灵活性
type agentServiceImpl struct {
	cfg          *config.Config
	runEngine    runtime.RunEngine
	memStore     memory.ShortTermStore
	registry     tool.Registry
	defaultAgent *domain.Agent
	credentials  credential.Service
	connections  connection.Service
}

// NewAgentService 创建 AgentService 实现（仅由 Container 调用）
func NewAgentService(
	cfg *config.Config,
	runEngine runtime.RunEngine,
	memStore memory.ShortTermStore,
	registry tool.Registry,
	defaultAgent *domain.Agent,
	credentials credential.Service,
	connections connection.Service,
) AgentService {
	return &agentServiceImpl{
		cfg:          cfg,
		runEngine:    runEngine,
		memStore:     memStore,
		registry:     registry,
		defaultAgent: defaultAgent,
		credentials:  credentials,
		connections:  connections,
	}
}

func (s *agentServiceImpl) Config() *config.Config {
	return s.cfg
}

func (s *agentServiceImpl) DefaultAgent() *domain.Agent {
	return s.defaultAgent
}

func (s *agentServiceImpl) Credentials() credential.Service {
	return s.credentials
}

func (s *agentServiceImpl) Connections() connection.Service {
	return s.connections
}
