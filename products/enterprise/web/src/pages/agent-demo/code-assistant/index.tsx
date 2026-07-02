/**
 * 代码助手演示页面
 * 展示 Agent 的代码生成、分析、优化能力
 * 使用 CodeHighlighter 组件高亮代码，Sender 支持文件上传
 */
import { useState, useCallback } from 'react';
import { Bubble, Sender, Welcome, Prompts, XProvider } from '@ant-design/x';
import { XMarkdown } from '@ant-design/x-markdown';
import {
  Avatar,
  Typography,
  Tag,
  Space,
  theme,
  App,
} from 'antd';
import {
  RobotOutlined,
  UserOutlined,
  CodeOutlined,
  BugOutlined,
  ThunderboltOutlined,
  FileTextOutlined,
} from '@ant-design/icons';
import { createStyles } from 'antd-style';
import { simulateStream } from '../qa/mockData';

const { useToken } = theme;

const useStyles = createStyles(({ token, css }) => ({
  layout: css`
    display: flex;
    flex-direction: column;
    height: calc(100vh - 64px);
    background: ${token.colorBgContainer};
  `,
  header: css`
    padding: 12px 24px;
    border-bottom: 1px solid ${token.colorBorderSecondary};
    background: ${token.colorBgElevated};
    display: flex;
    align-items: center;
    gap: 12px;
  `,
  messageList: css`
    flex: 1;
    overflow-y: auto;
    padding: 24px;
  `,
  inputArea: css`
    padding: 12px 24px 20px;
    border-top: 1px solid ${token.colorBorderSecondary};
  `,
}));

const CODE_PROMPTS = [
  { key: 'gen', label: '生成 Go gRPC 服务端代码', icon: <CodeOutlined /> },
  { key: 'review', label: '审查这段代码的潜在问题', icon: <BugOutlined /> },
  { key: 'perf', label: '优化这段 SQL 查询性能', icon: <ThunderboltOutlined /> },
  { key: 'doc', label: '为函数生成 Go doc 注释', icon: <FileTextOutlined /> },
];

const RESPONSES: Record<string, string> = {
  gen: `
## Go gRPC 服务端示例

\`\`\`protobuf
// agent.proto
syntax = "proto3";
package agent.v1;

service AgentService {
  rpc StartRun (StartRunRequest) returns (Run);
  rpc StreamRun (StreamRunRequest) returns (stream RunEvent);
  rpc GetRun (GetRunRequest) returns (Run);
}

message StartRunRequest {
  string tenant_id = 1;
  string session_id = 2;
  string input = 3;
}
\`\`\`

\`\`\`go
// server.go
package grpc

import (
    "context"
    
    agentv1 "github.com/yourorg/genesis-agent/gen/agent/v1"
    "github.com/yourorg/genesis-agent/internal/service"
)

type AgentServer struct {
    agentv1.UnimplementedAgentServiceServer
    runService service.RunService
}

// StartRun 启动一次 Agent 执行
func (s *AgentServer) StartRun(ctx context.Context, req *agentv1.StartRunRequest) (*agentv1.Run, error) {
    run, err := s.runService.Start(ctx, service.StartRunRequest{
        TenantID:  req.TenantId,
        SessionID: req.SessionId,
        Input:     req.Input,
    })
    if err != nil {
        return nil, fmt.Errorf("start run: %w", err)
    }
    return toProtoRun(run), nil
}
\`\`\`

> ✅ **已集成最佳实践**：错误包装、接口解耦、Proto 定义分离
  `.trim(),

  review: `
## 代码审查报告

我发现以下几个潜在问题：

### 🔴 高危问题

**1. SQL 注入风险**
\`\`\`go
// ❌ 危险：直接拼接 SQL
query := "SELECT * FROM users WHERE name = '" + name + "'"

// ✅ 修复：使用参数化查询
query := "SELECT * FROM users WHERE name = $1"
rows, err := db.QueryContext(ctx, query, name)
\`\`\`

### 🟡 中危问题

**2. 未处理 Context 取消**
\`\`\`go
// ❌ 忽略 ctx.Done()
for {
    item := queue.Pop()
    process(item)
}

// ✅ 修复：检查 context
for {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
        item := queue.Pop()
        if err := process(ctx, item); err != nil {
            return fmt.Errorf("process item: %w", err)
        }
    }
}
\`\`\`

### 🟢 建议优化

- 使用 \`errors.Is\` 替代字符串比较判断错误类型
- 添加结构化日志字段 \`run_id\`, \`tenant_id\`
- 考虑为热路径添加 OpenTelemetry Span
  `.trim(),

  perf: `
## SQL 查询优化建议

### 原始查询分析

\`\`\`sql
-- ❌ 性能问题：全表扫描 + N+1 查询
SELECT * FROM agent_run 
WHERE tenant_id = '123'
ORDER BY created_at DESC;
\`\`\`

### 优化后

\`\`\`sql
-- ✅ 优化：覆盖索引 + 分页
SELECT id, status, started_at, finished_at, total_tokens
FROM agent_run
WHERE tenant_id = $1
  AND status != 'cancelled'
ORDER BY created_at DESC
LIMIT 20 OFFSET $2;

-- 必要索引
CREATE INDEX CONCURRENTLY idx_agent_run_tenant_created 
ON agent_run(tenant_id, created_at DESC) 
WHERE status != 'cancelled';
\`\`\`

### 优化效果预估

| 指标 | 优化前 | 优化后 |
|------|--------|--------|
| 扫描行数 | 全表 ~100k | ~20 行 |
| 查询时间 | ~800ms | ~2ms |
| CPU | 高 | 极低 |

> 💡 建议同时开启 **pg_stat_statements** 监控慢查询
  `.trim(),

  doc: `
## 生成 Go doc 注释

\`\`\`go
// StartRun 启动一次 Agent 自主执行，创建 Run 记录并将其加入执行队列。
//
// 执行流程：
//  1. 验证请求参数（tenantID、agentInstanceID 必填）
//  2. 加载 AgentInstance 和 Agent 配置
//  3. 创建 Run 记录（状态 created）
//  4. 发布 run.started 事件
//  5. 异步调度 RunEngine 执行循环
//
// 参数：
//   - ctx: 上下文，应携带 tenant_id 和 trace_id
//   - req: 启动请求，包含 session_id、input、runtime_mode 等
//
// 返回：
//   - *Run: 创建成功的 Run 对象，Status 为 "running"
//   - error: 参数校验失败、实例不存在或 DB 错误时返回非 nil
//
// 注意：此方法是异步的，返回后 Agent 在后台继续执行。
// 通过 SSE /api/runs/:id/stream 订阅执行事件流。
func (s *runService) StartRun(ctx context.Context, req StartRunRequest) (*Run, error) {
    // ...
}
\`\`\`
  `.trim(),
};

export default function CodeAssistantPage() {
  const { styles } = useStyles();
  const { token } = useToken();

  interface Message {
    id: string;
    role: 'user' | 'assistant';
    content: string;
  }

  const [messages, setMessages] = useState<Message[]>([]);
  const [loading, setLoading] = useState(false);

  const handleSend = useCallback(
    (text: string) => {
      if (!text.trim() || loading) return;

      const lower = text.toLowerCase();
      let responseKey = 'gen';
      if (lower.includes('审查') || lower.includes('review') || lower.includes('问题')) responseKey = 'review';
      else if (lower.includes('优化') || lower.includes('sql') || lower.includes('性能')) responseKey = 'perf';
      else if (lower.includes('注释') || lower.includes('doc')) responseKey = 'doc';

      const userMsg: Message = { id: `u-${Date.now()}`, role: 'user', content: text };
      const assistantId = `a-${Date.now()}`;
      const assistantMsg: Message = { id: assistantId, role: 'assistant', content: '' };

      setMessages((prev) => [...prev, userMsg, assistantMsg]);
      setLoading(true);

      let accumulated = '';
      simulateStream(
        RESPONSES[responseKey],
        (chunk, done) => {
          if (done) { setLoading(false); return; }
          accumulated += chunk;
          setMessages((prev) => prev.map((m) => m.id === assistantId ? { ...m, content: accumulated } : m));
        },
        20,
      );
    },
    [loading],
  );

  return (
    <XProvider locale={{ locale: 'zh-CN' }}>
      <App>
        <div className={styles.layout}>
          <div className={styles.header}>
            <Avatar size="small" icon={<CodeOutlined />} style={{ background: '#13c2c2' }} />
            <Typography.Title level={5} style={{ margin: 0 }}>
              代码助手
            </Typography.Title>
            <Space size={4}>
              {(['Go', 'SQL', 'Proto', 'Shell'] as string[]).map((lang) => (
                <Tag key={lang} color="cyan">{lang}</Tag>
              ))}
            </Space>
          </div>

          <div className={styles.messageList}>
            {messages.length === 0 ? (
              <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 24, paddingTop: 40 }}>
                <Welcome
                  icon={<Avatar size={64} icon={<CodeOutlined />} style={{ background: '#13c2c2' }} />}
                  title="代码助手"
                  description="我可以帮你生成代码、审查代码、优化 SQL、生成文档注释"
                />
                <Prompts
                  title="快速开始："
                  items={CODE_PROMPTS.map((p) => ({ key: p.key, label: p.label, icon: p.icon }))}
                  onItemClick={(item) => handleSend(item.data.label as string)}
                  wrap
                />
              </div>
            ) : (
              <Bubble.List
                items={messages.map((msg) => ({
                  key: msg.id,
                  role: msg.role,
                  content: <XMarkdown content={msg.content || '...'} />,
                  avatar:
                    msg.role === 'user'
                      ? <Avatar icon={<UserOutlined />} />
                      : <Avatar icon={<RobotOutlined />} style={{ background: '#13c2c2' }} />,
                  placement: msg.role === 'user' ? 'end' : 'start',
                  loading: !msg.content && msg.role === 'assistant',
                }))}
              />
            )}
          </div>

          <div className={styles.inputArea}>
            <Sender
              placeholder="描述你需要的代码功能，或粘贴代码请求审查..."
              loading={loading}
              onSubmit={handleSend}
              onCancel={() => setLoading(false)}
            />
          </div>
        </div>
      </App>
    </XProvider>
  );
}
