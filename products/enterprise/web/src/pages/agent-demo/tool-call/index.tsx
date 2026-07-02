/**
 * 工具调用演示页面
 * 可视化展示 Agent 调用各类工具的完整流程：
 * 权限校验 → 执行 → 观察结果 → 继续推理
 */
import { useState, useCallback } from 'react';
import { Bubble, Sender, Welcome, XProvider } from '@ant-design/x';
import { XMarkdown } from '@ant-design/x-markdown';
import {
  Avatar,
  Typography,
  Card,
  Tag,
  Space,
  Steps,
  Timeline,
  theme,
  App,
  Divider,
  Badge,
} from 'antd';
import {
  RobotOutlined,
  UserOutlined,
  ToolOutlined,
  CheckCircleOutlined,
  LoadingOutlined,
  LockOutlined,
  SendOutlined,
  EyeOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import { createStyles } from 'antd-style';
import { simulateStream } from '../qa/mockData';

const { useToken } = theme;

const useStyles = createStyles(({ token, css }) => ({
  layout: css`
    display: flex;
    height: calc(100vh - 64px);
    background: ${token.colorBgContainer};
    overflow: hidden;
  `,
  toolPanel: css`
    width: 280px;
    min-width: 280px;
    border-right: 1px solid ${token.colorBorderSecondary};
    padding: 16px;
    overflow-y: auto;
    background: ${token.colorBgContainer};
  `,
  chatArea: css`
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
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

/** 可用工具列表 */
const AVAILABLE_TOOLS = [
  {
    id: 'web_search',
    name: 'web_search',
    description: '搜索互联网获取最新信息',
    permission: 'none',
    icon: '🔍',
    category: 'Search',
  },
  {
    id: 'code_runner',
    name: 'code_runner',
    description: '在沙盒中执行代码片段',
    permission: 'confirm',
    icon: '▶️',
    category: 'Execution',
  },
  {
    id: 'read_file',
    name: 'read_file',
    description: '读取工作目录中的文件',
    permission: 'none',
    icon: '📄',
    category: 'FileSystem',
  },
  {
    id: 'write_file',
    name: 'write_file',
    description: '写入或修改工作目录文件',
    permission: 'confirm',
    icon: '✏️',
    category: 'FileSystem',
  },
  {
    id: 'api_call',
    name: 'api_call',
    description: '调用外部 REST API',
    permission: 'approval',
    icon: '🌐',
    category: 'Network',
  },
  {
    id: 'db_query',
    name: 'db_query',
    description: '执行只读数据库查询',
    permission: 'none',
    icon: '🗄️',
    category: 'Database',
  },
];

const permissionConfig = {
  none: { color: 'success', label: '直接执行' },
  confirm: { color: 'warning', label: '需确认' },
  approval: { color: 'error', label: '需审批' },
};

/** 工具调用执行记录 */
interface ToolExecution {
  id: string;
  toolName: string;
  icon: string;
  permission: string;
  status: 'checking' | 'waiting' | 'running' | 'done' | 'error';
  input: string;
  output?: string;
  duration?: number;
}

const DEMO_RESPONSE = `
## 执行结果

我已依次完成以下工具调用：

### 1. web_search — 查询最新 Agent 框架对比

**搜索结果摘要**（2026年）：
- **LangGraph**：图结构 Agent，支持复杂分支逻辑
- **Eino（CloudWeGo）**：Go 原生，高性能，接近 Genesis 架构
- **AutoGen**：多 Agent 协作，微软出品
- **Genesis Agent**：本项目，专注生产级可靠性

### 2. db_query — 统计 Agent 运行数据

| 指标 | 数值 |
|------|------|
| 今日总 Run 数 | 1,247 |
| 成功率 | 94.2% |
| 平均执行时长 | 8.3s |
| 平均 Token 消耗 | 2,847 |

### 3. code_runner — 验证计算逻辑

\`\`\`
执行结果：
成功率趋势（最近7天）：[92.1, 93.4, 91.8, 94.2, 95.1, 94.8, 94.2]
峰值时间：每日 10:00-11:00（北京时间）
\`\`\`

> ✅ 所有工具调用已完成，权限校验通过
`.trim();

export default function ToolCallPage() {
  const { styles } = useStyles();
  const { token } = useToken();

  interface Message {
    id: string;
    role: 'user' | 'assistant';
    content: string;
    toolExecutions?: ToolExecution[];
  }

  const [messages, setMessages] = useState<Message[]>([]);
  const [loading, setLoading] = useState(false);
  const [activeTools, setActiveTools] = useState<Set<string>>(new Set());

  const handleSend = useCallback(
    (text: string) => {
      if (!text.trim() || loading) return;

      const userMsg: Message = { id: `u-${Date.now()}`, role: 'user', content: text };
      const assistantId = `a-${Date.now()}`;
      const executions: ToolExecution[] = [
        {
          id: 'e1',
          toolName: 'web_search',
          icon: '🔍',
          permission: 'none',
          status: 'checking',
          input: 'Agent framework comparison 2026',
        },
        {
          id: 'e2',
          toolName: 'db_query',
          icon: '🗄️',
          permission: 'none',
          status: 'checking',
          input: 'SELECT * FROM agent_run_stats WHERE date = CURRENT_DATE',
        },
        {
          id: 'e3',
          toolName: 'code_runner',
          icon: '▶️',
          permission: 'confirm',
          status: 'checking',
          input: 'python: analyze_trend(data)',
        },
      ];

      const assistantMsg: Message = {
        id: assistantId,
        role: 'assistant',
        content: '',
        toolExecutions: executions,
      };

      setMessages((prev) => [...prev, userMsg, assistantMsg]);
      setLoading(true);

      // 模拟工具执行流程
      const updateExec = (id: string, update: Partial<ToolExecution>) => {
        setMessages((prev) =>
          prev.map((m) =>
            m.id === assistantId
              ? {
                  ...m,
                  toolExecutions: m.toolExecutions?.map((e) =>
                    e.id === id ? { ...e, ...update } : e,
                  ),
                }
              : m,
          ),
        );
      };

      const steps = [
        // e1: 权限通过 → 执行
        () => updateExec('e1', { status: 'running' }),
        () => {
          setActiveTools((s) => new Set(s).add('web_search'));
          updateExec('e1', {
            status: 'done',
            output: '找到 12 个相关结果，提取 4 个框架对比',
            duration: 820,
          });
          setActiveTools((s) => {
            const n = new Set(s);
            n.delete('web_search');
            return n;
          });
        },
        // e2: 执行数据库查询
        () => updateExec('e2', { status: 'running' }),
        () => {
          setActiveTools((s) => new Set(s).add('db_query'));
          updateExec('e2', {
            status: 'done',
            output: '返回 4 行统计数据',
            duration: 45,
          });
          setActiveTools((s) => {
            const n = new Set(s);
            n.delete('db_query');
            return n;
          });
        },
        // e3: 需要确认
        () => updateExec('e3', { status: 'waiting' }),
        // 模拟用户自动确认
        () => updateExec('e3', { status: 'running' }),
        () => {
          setActiveTools((s) => new Set(s).add('code_runner'));
          updateExec('e3', {
            status: 'done',
            output: '代码执行成功，返回趋势数组',
            duration: 310,
          });
          setActiveTools((s) => {
            const n = new Set(s);
            n.delete('code_runner');
            return n;
          });
        },
        // 开始输出答案
        () => {
          let accumulated = '';
          simulateStream(
            DEMO_RESPONSE,
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

      steps.forEach((step, i) => setTimeout(step, i * 700));
    },
    [loading],
  );

  const renderToolExecutions = (executions: ToolExecution[]) => (
    <div style={{ marginBottom: 16 }}>
      <Card
        size="small"
        title={
          <Space>
            <ToolOutlined style={{ color: token.colorPrimary }} />
            <Typography.Text strong>工具调用链</Typography.Text>
          </Space>
        }
        style={{ background: token.colorFillAlter, border: `1px solid ${token.colorBorderSecondary}` }}
      >
        <Timeline
          items={executions.map((exec) => {
            const isLoading = exec.status === 'running' || exec.status === 'checking';
            const isWaiting = exec.status === 'waiting';
            const isDone = exec.status === 'done';

            return {
              dot: isLoading ? (
                <LoadingOutlined style={{ color: token.colorPrimary }} />
              ) : isDone ? (
                <CheckCircleOutlined style={{ color: token.colorSuccess }} />
              ) : isWaiting ? (
                <LockOutlined style={{ color: token.colorWarning }} />
              ) : (
                <div style={{ width: 8, height: 8, borderRadius: '50%', background: token.colorBorderSecondary }} />
              ),
              children: (
                <div>
                  <Space size={4} wrap>
                    <Typography.Text strong style={{ fontSize: 13 }}>
                      {exec.icon} {exec.toolName}
                    </Typography.Text>
                    <Tag
                      color={permissionConfig[exec.permission as keyof typeof permissionConfig]?.color}
                      style={{ fontSize: 11 }}
                    >
                      {permissionConfig[exec.permission as keyof typeof permissionConfig]?.label}
                    </Tag>
                    {isWaiting && <Tag color="warning">等待用户确认...</Tag>}
                    {isDone && exec.duration && (
                      <Tag color="default">{exec.duration}ms</Tag>
                    )}
                  </Space>
                  <Typography.Text type="secondary" style={{ fontSize: 11, display: 'block' }}>
                    输入：{exec.input}
                  </Typography.Text>
                  {exec.output && (
                    <Typography.Text style={{ fontSize: 11, color: token.colorSuccess, display: 'block' }}>
                      ✓ {exec.output}
                    </Typography.Text>
                  )}
                </div>
              ),
            };
          })}
        />
      </Card>
    </div>
  );

  return (
    <XProvider locale={{ locale: 'zh-CN' }}>
      <App>
        <div className={styles.layout}>
          {/* 左侧工具面板 */}
          <div className={styles.toolPanel}>
            <Typography.Title level={5} style={{ marginBottom: 16 }}>
              <ToolOutlined style={{ marginRight: 8 }} />
              可用工具
            </Typography.Title>
            <Space direction="vertical" style={{ width: '100%' }} size={8}>
              {AVAILABLE_TOOLS.map((tool) => (
                <Card
                  key={tool.id}
                  size="small"
                  styles={{ body: { padding: '8px 12px' } }}
                  style={{
                    border: `1px solid ${activeTools.has(tool.name) ? token.colorPrimary : token.colorBorderSecondary}`,
                    transition: 'border-color 0.2s',
                  }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                    <div style={{ flex: 1 }}>
                      <Space size={4}>
                        <span>{tool.icon}</span>
                        <Typography.Text strong style={{ fontSize: 12 }}>
                          {tool.name}
                        </Typography.Text>
                        {activeTools.has(tool.name) && (
                          <Badge status="processing" />
                        )}
                      </Space>
                      <Typography.Text
                        type="secondary"
                        style={{ fontSize: 11, display: 'block', marginTop: 2 }}
                      >
                        {tool.description}
                      </Typography.Text>
                    </div>
                    <Tag
                      color={permissionConfig[tool.permission as keyof typeof permissionConfig]?.color}
                      style={{ fontSize: 10, flexShrink: 0, marginLeft: 4 }}
                    >
                      {permissionConfig[tool.permission as keyof typeof permissionConfig]?.label}
                    </Tag>
                  </div>
                </Card>
              ))}
            </Space>

            <Divider />
            <Typography.Text type="secondary" style={{ fontSize: 11 }}>
              权限说明：
              <br />• 直接执行：无需确认
              <br />• 需确认：用户确认后执行
              <br />• 需审批：管理员审批后执行
            </Typography.Text>
          </div>

          {/* 右侧聊天区域 */}
          <div className={styles.chatArea}>
            <div className={styles.messageList}>
              {messages.length === 0 ? (
                <Welcome
                  icon={<Avatar size={64} icon={<ToolOutlined />} style={{ background: '#fa8c16' }} />}
                  title="工具调用演示"
                  description="体验 Agent 的工具调用链：权限校验 → 执行 → 观察。试问：「帮我查询今天的 Agent 运行统计」"
                  style={{ maxWidth: 480, margin: '40px auto' }}
                />
              ) : (
                <Bubble.List
                  items={messages.map((msg) => ({
                    key: msg.id,
                    role: msg.role,
                    content: (
                      <div>
                        {msg.role === 'assistant' && msg.toolExecutions && renderToolExecutions(msg.toolExecutions)}
                        {msg.content && <XMarkdown content={msg.content} />}
                      </div>
                    ),
                    avatar:
                      msg.role === 'user'
                        ? <Avatar icon={<UserOutlined />} />
                        : <Avatar icon={<RobotOutlined />} style={{ background: '#fa8c16' }} />,
                    placement: msg.role === 'user' ? 'end' : 'start',
                  }))}
                />
              )}
            </div>

            <div className={styles.inputArea}>
              <Sender
                placeholder="输入任务，Agent 将自动选择并调用合适的工具..."
                loading={loading}
                onSubmit={handleSend}
                onCancel={() => setLoading(false)}
              />
            </div>
          </div>
        </div>
      </App>
    </XProvider>
  );
}
