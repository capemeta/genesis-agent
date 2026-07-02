/**
 * RAG 知识问答演示页面
 * 展示 Agentic RAG 的多轮检索、覆盖度评估、并行检索流程
 */
import { useState, useCallback } from 'react';
import { Bubble, Sender, Welcome, XProvider } from '@ant-design/x';
import { XMarkdown } from '@ant-design/x-markdown';
import {
  Avatar,
  Card,
  Tag,
  Progress,
  Space,
  Typography,
  Timeline,
  theme,
  App,
  Divider,
} from 'antd';
import {
  RobotOutlined,
  UserOutlined,
  DatabaseOutlined,
  SearchOutlined,
  CheckCircleOutlined,
  LoadingOutlined,
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
    padding: 16px 24px;
    border-bottom: 1px solid ${token.colorBorderSecondary};
    background: ${token.colorBgElevated};
  `,
  messageList: css`
    flex: 1;
    overflow-y: auto;
    padding: 24px;
  `,
  inputArea: css`
    padding: 16px 24px 24px;
    border-top: 1px solid ${token.colorBorderSecondary};
  `,
}));

/** 模拟 RAG 检索过程 */
interface RAGProcess {
  round: number;
  queries: string[];
  sources: Array<{ name: string; score: number; excerpt: string }>;
  coverage: number;
  done: boolean;
}

/** 知识库配置 */
const knowledgeBases = [
  { name: 'Genesis Agent 设计文档', icon: '📐', color: 'blue' },
  { name: 'Go 最佳实践', icon: '🔧', color: 'green' },
  { name: 'Agent 论文库', icon: '📚', color: 'purple' },
];

const RAG_RESPONSE = `
## Agent 记忆系统设计

基于知识库检索，Genesis Agent 的记忆系统分为**四个层次**：

### 层次架构

| 层次 | 类型 | 生命周期 | 实现方式 |
|------|------|----------|----------|
| **工作记忆** | key-value | Run 内存 | RunContext 字段 |
| **短期记忆** | 对话历史 | Session 级 | DB / Redis |
| **长期记忆** | 语义向量 | 跨 Session | PostgreSQL + pgvector |
| **静态上下文** | 注入式 | 每次 Run | Config / DB / API |

### 注入顺序

\`\`\`
StaticContext → LongTermMemory → ShortTermHistory → WorkingMemory → 当前输入
\`\`\`

### 长期记忆检索

使用 **pgvector** 进行语义相似度检索：

\`\`\`sql
SELECT id, content, metadata,
       1 - (embedding <=> query_embedding) AS similarity
FROM agent_memory
WHERE tenant_id = $1
  AND agent_instance_id = $2
  AND 1 - (embedding <=> $3) > 0.7
ORDER BY similarity DESC
LIMIT 5;
\`\`\`

> 📌 **来源**：已从 3 个知识库检索 8 个相关片段，覆盖度评分 92%
`.trim();

export default function RAGPage() {
  const { styles } = useStyles();
  const { token } = useToken();

  interface Message {
    id: string;
    role: 'user' | 'assistant';
    content: string;
    ragProcess?: RAGProcess;
  }

  const [messages, setMessages] = useState<Message[]>([]);
  const [loading, setLoading] = useState(false);

  const handleSend = useCallback(
    (text: string) => {
      if (!text.trim() || loading) return;

      const userMsg: Message = {
        id: `user-${Date.now()}`,
        role: 'user',
        content: text,
      };

      const assistantId = `assistant-${Date.now()}`;
      const assistantMsg: Message = {
        id: assistantId,
        role: 'assistant',
        content: '',
        ragProcess: {
          round: 1,
          queries: [],
          sources: [],
          coverage: 0,
          done: false,
        },
      };

      setMessages((prev) => [...prev, userMsg, assistantMsg]);
      setLoading(true);

      // 模拟 RAG 检索过程
      const ragSteps = [
        // 第1轮：生成检索词
        () =>
          setMessages((prev) =>
            prev.map((m) =>
              m.id === assistantId
                ? {
                    ...m,
                    ragProcess: {
                      round: 1,
                      queries: ['Agent 记忆系统', 'memory store architecture'],
                      sources: [],
                      coverage: 0,
                      done: false,
                    },
                  }
                : m,
            ),
          ),
        // 第1轮：获得初步结果
        () =>
          setMessages((prev) =>
            prev.map((m) =>
              m.id === assistantId
                ? {
                    ...m,
                    ragProcess: {
                      round: 1,
                      queries: ['Agent 记忆系统', 'memory store architecture'],
                      sources: [
                        {
                          name: 'Genesis Agent 设计文档 §5.4',
                          score: 0.91,
                          excerpt: '记忆系统分为四个层次：WorkingMemory...',
                        },
                        {
                          name: 'Agent 论文库 - MemGPT',
                          score: 0.78,
                          excerpt: 'Memory hierarchy for LLM agents...',
                        },
                      ],
                      coverage: 60,
                      done: false,
                    },
                  }
                : m,
            ),
          ),
        // 第2轮：补充检索
        () =>
          setMessages((prev) =>
            prev.map((m) =>
              m.id === assistantId
                ? {
                    ...m,
                    ragProcess: {
                      round: 2,
                      queries: ['pgvector 语义检索', 'Session 对话历史压缩'],
                      sources: [
                        {
                          name: 'Genesis Agent 设计文档 §5.4',
                          score: 0.91,
                          excerpt: '记忆系统分为四个层次：WorkingMemory...',
                        },
                        {
                          name: 'Agent 论文库 - MemGPT',
                          score: 0.78,
                          excerpt: 'Memory hierarchy for LLM agents...',
                        },
                        {
                          name: 'Go 最佳实践 - pgvector',
                          score: 0.85,
                          excerpt: 'Using pgvector for semantic search...',
                        },
                      ],
                      coverage: 92,
                      done: true,
                    },
                  }
                : m,
            ),
          ),
        // 开始流式输出答案
        () => {
          let accumulated = '';
          simulateStream(
            RAG_RESPONSE,
            (chunk, done) => {
              if (done) {
                setLoading(false);
                return;
              }
              accumulated += chunk;
              setMessages((prev) =>
                prev.map((m) =>
                  m.id === assistantId ? { ...m, content: accumulated } : m,
                ),
              );
            },
            20,
          );
        },
      ];

      // 依次执行各步骤
      ragSteps.forEach((step, i) => {
        setTimeout(step, i * 900);
      });
    },
    [loading],
  );

  const renderRAGProcess = (ragProcess: RAGProcess) => (
    <div style={{ marginBottom: 16 }}>
      <Card
        size="small"
        title={
          <Space>
            <DatabaseOutlined style={{ color: token.colorPrimary }} />
            <Typography.Text strong>RAG 检索过程</Typography.Text>
            {!ragProcess.done && <LoadingOutlined style={{ color: token.colorPrimary }} />}
            {ragProcess.done && (
              <CheckCircleOutlined style={{ color: token.colorSuccess }} />
            )}
          </Space>
        }
        style={{ background: token.colorFillAlter, border: `1px solid ${token.colorBorderSecondary}` }}
      >
        <Space direction="vertical" style={{ width: '100%' }} size={12}>
          {/* 检索轮次 */}
          <div>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              第 {ragProcess.round} 轮检索
            </Typography.Text>
            <Space wrap style={{ marginTop: 4 }}>
              {ragProcess.queries.map((q) => (
                <Tag key={q} icon={<SearchOutlined />} color="blue">
                  {q}
                </Tag>
              ))}
            </Space>
          </div>

          {/* 检索到的知识源 */}
          {ragProcess.sources.length > 0 && (
            <div>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                检索来源（{ragProcess.sources.length} 个）
              </Typography.Text>
              <Space direction="vertical" style={{ width: '100%', marginTop: 4 }} size={4}>
                {ragProcess.sources.map((src) => (
                  <div
                    key={src.name}
                    style={{
                      background: token.colorBgContainer,
                      border: `1px solid ${token.colorBorderSecondary}`,
                      borderRadius: token.borderRadius,
                      padding: '4px 8px',
                    }}
                  >
                    <Space>
                      <Tag color="green" style={{ marginRight: 0 }}>
                        {(src.score * 100).toFixed(0)}%
                      </Tag>
                      <Typography.Text style={{ fontSize: 12 }}>{src.name}</Typography.Text>
                    </Space>
                    <Typography.Text
                      type="secondary"
                      style={{ fontSize: 11, display: 'block', marginTop: 2 }}
                    >
                      {src.excerpt}
                    </Typography.Text>
                  </div>
                ))}
              </Space>
            </div>
          )}

          {/* 覆盖度评分 */}
          {ragProcess.coverage > 0 && (
            <div>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                知识覆盖度
              </Typography.Text>
              <Progress
                percent={ragProcess.coverage}
                size="small"
                status={ragProcess.coverage >= 80 ? 'success' : 'active'}
                style={{ marginTop: 4 }}
              />
            </div>
          )}
        </Space>
      </Card>
    </div>
  );

  return (
    <XProvider locale={{ locale: 'zh-CN' }}>
      <App>
        <div className={styles.layout}>
          {/* 页头说明 */}
          <div className={styles.header}>
            <Space>
              <DatabaseOutlined style={{ fontSize: 18, color: token.colorPrimary }} />
              <Typography.Title level={5} style={{ margin: 0 }}>
                RAG 知识问答
              </Typography.Title>
              <Divider type="vertical" />
              <Space size={4}>
                {knowledgeBases.map((kb) => (
                  <Tag key={kb.name} color={kb.color} icon={<span>{kb.icon}</span>}>
                    {kb.name}
                  </Tag>
                ))}
              </Space>
            </Space>
          </div>

          {/* 消息列表 */}
          <div className={styles.messageList}>
            {messages.length === 0 ? (
              <Welcome
                icon={<Avatar size={64} icon={<DatabaseOutlined />} style={{ background: '#722ed1' }} />}
                title="RAG 知识问答"
                description="基于多知识库的自主检索增强问答，支持多轮检索直到覆盖度满足阈值。试问：「Genesis Agent 的记忆系统是如何设计的？」"
              />
            ) : (
              <Bubble.List
                items={messages.map((msg) => ({
                  key: msg.id,
                  role: msg.role,
                  content: (
                    <div>
                      {msg.role === 'assistant' && msg.ragProcess && renderRAGProcess(msg.ragProcess)}
                      {msg.content && <XMarkdown content={msg.content} />}
                    </div>
                  ),
                  avatar:
                    msg.role === 'user' ? (
                      <Avatar icon={<UserOutlined />} />
                    ) : (
                      <Avatar icon={<RobotOutlined />} style={{ background: '#722ed1' }} />
                    ),
                  placement: msg.role === 'user' ? 'end' : 'start',
                }))}
              />
            )}
          </div>

          {/* 输入框 */}
          <div className={styles.inputArea}>
            <Sender
              placeholder="提问关于知识库的问题，Agent 将自动多轮检索..."
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
