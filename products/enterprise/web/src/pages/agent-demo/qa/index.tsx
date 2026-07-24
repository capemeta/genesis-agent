/**
 * 智能问答页面
 * 使用 @ant-design/x 组件库搭建完整的 Agent 对话界面
 * 支持：Markdown 渲染、思维链展示、工具调用状态、结构化卡片
 */
import { useState, useRef, useCallback } from 'react';
import { Bubble, Conversations, Prompts, Sender, Welcome, XProvider } from '@ant-design/x';
import { XMarkdown } from '@ant-design/x-markdown';
import { Avatar, Button, Space, Typography, theme, Divider, message } from 'antd';
import {
  RobotOutlined,
  UserOutlined,
  PlusOutlined,
  ClearOutlined,
  DeleteOutlined,
  PaperClipOutlined,
} from '@ant-design/icons';
import { createStyles } from 'antd-style';
import ThinkingProcess from './components/ThinkingProcess';
import ToolCallBadge from './components/ToolCallBadge';
import PlanCard, { parsePlanFromCommands } from './components/PlanCard';
import {
  mockConversations,
  mockPrompts,
  detectScenario,
  simulateStream,
  thinkingSteps,
  thinkingResponse,
  markdownResponse,
  toolCallResponse,
  cardResponse,
  type ThoughtStep,
} from './mockData';
import type { ToolCallRecord } from './components/ToolCallBadge';
import { API_BASE, runWithAttachments, uploadFile, type UploadedAttachment } from './attachLive';
const { useToken } = theme;

// ── 自定义消息类型 ────────────────────────────────────────────────────────────

interface ChatMessage {
  id: string;
  /** Bubble.List 使用的 role 字段 */
  role: 'user' | 'ai';
  /** 消息文本内容 */
  content: string;
  /** 思维链步骤 */
  thinkingSteps?: ThoughtStep[];
  /** 工具调用记录 */
  toolCalls?: ToolCallRecord[];
  /** 是否展示结构化卡片 */
  showCard?: boolean;
  /** 是否正在流式输出 */
  streaming?: boolean;
}

// ── 样式 ────────────────────────────────────────────────────────────────────

const useStyles = createStyles(({ token, css }) => ({
  layout: css`
    display: flex;
    height: calc(100vh - 64px);
    background: ${token.colorBgContainer};
    overflow: hidden;
  `,
  sider: css`
    width: 240px;
    min-width: 240px;
    border-right: 1px solid ${token.colorBorderSecondary};
    display: flex;
    flex-direction: column;
    background: ${token.colorBgContainer};
  `,
  siderHeader: css`
    padding: 16px 12px 8px;
    display: flex;
    justify-content: space-between;
    align-items: center;
  `,
  siderContent: css`
    flex: 1;
    overflow-y: auto;
    padding: 0 8px;
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
    padding: 16px 24px 24px;
    border-top: 1px solid ${token.colorBorderSecondary};
    background: ${token.colorBgContainer};
  `,
  welcomeWrapper: css`
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    padding-bottom: 40px;
  `,
  agentAvatar: css`
    background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
    display: flex;
    align-items: center;
    justify-content: center;
  `,
}));

// ── 主组件 ────────────────────────────────────────────────────────────────────

export default function QAPage() {
  const { styles } = useStyles();
  const { token } = useToken();

  const [activeConvKey, setActiveConvKey] = useState<string>('conv-1');
  const [conversations, setConversations] = useState(mockConversations);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [loading, setLoading] = useState(false);
  const [liveMode, setLiveMode] = useState(false);
  const [pendingFiles, setPendingFiles] = useState<UploadedAttachment[]>([]);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const stopStreamRef = useRef<(() => void) | null>(null);

  /** 更新指定消息的部分字段 */
  const updateMessage = useCallback((id: string, update: Partial<ChatMessage>) => {
    setMessages((prev) => prev.map((m) => (m.id === id ? { ...m, ...update } : m)));
  }, []);

  const handlePickFiles = async (fileList: FileList | null) => {
    if (!fileList?.length) return;
    try {
      const uploaded: UploadedAttachment[] = [];
      for (const file of Array.from(fileList)) {
        uploaded.push(await uploadFile(file));
      }
      setPendingFiles((prev) => [...prev, ...uploaded]);
      message.success(`已上传 ${uploaded.length} 个文件（仅 id，无 StartRun base64）`);
    } catch (e) {
      message.error(e instanceof Error ? e.message : String(e));
    }
  };

  /** 发送消息 */
  const handleSend = useCallback(
    (text: string) => {
      if (!text.trim() || loading) return;

      if (liveMode) {
        const userMsg: ChatMessage = { id: `user-${Date.now()}`, role: 'user', content: text };
        const assistantId = `ai-${Date.now() + 1}`;
        setMessages((prev) => [
          ...prev,
          userMsg,
          { id: assistantId, role: 'ai', content: '', streaming: true },
        ]);
        setLoading(true);
        const atts = [...pendingFiles];
        setPendingFiles([]);
        void runWithAttachments(text, atts)
          .then((res) => {
            updateMessage(assistantId, {
              content: res.answer || `(status=${res.status})`,
              streaming: false,
            });
          })
          .catch((e) => {
            updateMessage(assistantId, {
              content: `错误: ${e instanceof Error ? e.message : String(e)}`,
              streaming: false,
            });
          })
          .finally(() => setLoading(false));
        return;
      }

      const scenario = detectScenario(text);
      const userMsg: ChatMessage = { id: `user-${Date.now()}`, role: 'user', content: text };
      const assistantId = `ai-${Date.now() + 1}`;
      const assistantMsg: ChatMessage = {
        id: assistantId,
        role: 'ai',
        content: '',
        streaming: true,
      };

      setMessages((prev) => [...prev, userMsg, assistantMsg]);
      setLoading(true);

      if (scenario === 'thinking' || scenario === 'card') {
        // 先显示思维链，步骤依次完成后开始流式输出
        updateMessage(assistantId, {
          thinkingSteps: thinkingSteps.map((s, i) => ({
            ...s,
            status: i === 0 ? ('loading' as const) : undefined,
          })),
        });

        let stepIdx = 0;
        const stepInterval = setInterval(() => {
          stepIdx++;
          if (stepIdx >= thinkingSteps.length) {
            clearInterval(stepInterval);
            // 所有步骤标记为完成后再开始流式输出
            updateMessage(assistantId, {
              thinkingSteps: thinkingSteps.map((s) => ({ ...s, status: 'success' as const })),
            });
            // 短暂延迟让用户看到全部完成状态
            setTimeout(() => startStreaming(scenario, assistantId), 300);
          } else {
            updateMessage(assistantId, {
              thinkingSteps: thinkingSteps.map((s, i) => ({
                ...s,
                status:
                  i < stepIdx
                    ? ('success' as const)
                    : i === stepIdx
                      ? ('loading' as const)
                      : undefined,
              })),
            });
          }
        }, 700);
      } else if (scenario === 'tool-call') {
        const toolId = `tool-${Date.now()}`;
        updateMessage(assistantId, {
          toolCalls: [
            {
              id: toolId,
              name: 'code_generator',
              type: 'code_exec',
              status: 'calling',
              input: 'HTTP middleware with OpenTelemetry tracing',
            },
          ],
        });

        setTimeout(() => {
          updateMessage(assistantId, {
            toolCalls: [
              {
                id: toolId,
                name: 'code_generator',
                type: 'code_exec',
                status: 'done',
                input: 'HTTP middleware with OpenTelemetry tracing',
                output: 'Generated 45 lines of Go code',
                duration: 1240,
              },
            ],
          });
          startStreaming(scenario, assistantId);
        }, 1500);
      } else {
        startStreaming(scenario, assistantId);
      }

      function startStreaming(sc: string, msgId: string) {
        const responseText =
          sc === 'thinking'
            ? thinkingResponse
            : sc === 'tool-call'
              ? toolCallResponse
              : sc === 'card'
                ? cardResponse
                : markdownResponse;

        let accumulated = '';
        const stop = simulateStream(
          responseText,
          (chunk, done) => {
            if (done) {
              setMessages((prev) =>
                prev.map((m) => {
                  if (m.id !== msgId) return m;
                  return {
                    ...m,
                    streaming: false,
                    showCard: sc === 'card',
                    // 确保所有思维链步骤都标记为完成
                    thinkingSteps: m.thinkingSteps?.map((s) => ({
                      ...s,
                      status: 'success' as const,
                    })),
                  };
                }),
              );
              setLoading(false);
              stopStreamRef.current = null;
              return;
            }
            accumulated += chunk;
            setMessages((prev) =>
              prev.map((m) => (m.id === msgId ? { ...m, content: accumulated } : m)),
            );
          },
          25,
        );
        stopStreamRef.current = stop;
      }
    },
    [loading, updateMessage, liveMode, pendingFiles],
  );

  const handleStop = () => {
    stopStreamRef.current?.();
    stopStreamRef.current = null;
    setLoading(false);
  };

  const handleNewConversation = () => {
    const key = `conv-${Date.now()}`;
    setConversations((prev) => [{ key, label: '新对话', timestamp: Date.now() }, ...prev]);
    setActiveConvKey(key);
    setMessages([]);
  };

  const handleDeleteConversation = (key: string) => {
    setConversations((prev) => prev.filter((c) => c.key !== key));
    if (activeConvKey === key) {
      const remaining = conversations.filter((c) => c.key !== key);
      if (remaining.length > 0) setActiveConvKey(remaining[0].key);
    }
  };

  // ── 渲染气泡内容 ─────────────────────────────────────────────────────────

  const renderContent = (msg: ChatMessage) => {
    if (msg.role === 'user') {
      return <Typography.Text>{msg.content}</Typography.Text>;
    }

    const hasThinkingInProgress = msg.thinkingSteps?.some((s) => s.status === 'loading');

    return (
      <div style={{ width: '100%' }}>
        {/* Markdown 内容 (最终内容放上面) */}
        {msg.content && <XMarkdown content={msg.content} />}

        {/* 流式光标 */}
        {msg.streaming && !hasThinkingInProgress && (
          <span
            style={{
              display: 'inline-block',
              width: 2,
              height: '1em',
              background: token.colorPrimary,
              marginLeft: 2,
              animation: 'cursor-blink 1s step-end infinite',
            }}
          />
        )}

        {/* 工具调用状态 */}
        {msg.toolCalls && <ToolCallBadge tools={msg.toolCalls} />}

        {/* 思维链 (思考过程放下面) */}
        {msg.thinkingSteps && (
          <ThinkingProcess steps={msg.thinkingSteps} running={hasThinkingInProgress} />
        )}

        {/* 结构化卡片 */}
        {msg.showCard && (
          <div style={{ marginTop: 16 }}>
            <Divider style={{ margin: '8px 0' }} />
            <PlanCard {...parsePlanFromCommands([])} />
          </div>
        )}
      </div>
    );
  };

  // ── 渲染 ────────────────────────────────────────────────────────────────────

  const hasMessages = messages.length > 0;

  return (
    <XProvider locale={{ locale: 'zh-CN' }}>
      <div className={styles.layout}>
        {/* 左侧会话列表 */}
        <div className={styles.sider}>
          <div className={styles.siderHeader}>
            <Typography.Text strong style={{ fontSize: 14 }}>
              对话列表
            </Typography.Text>
            <Button
              type="text"
              size="small"
              icon={<PlusOutlined />}
              onClick={handleNewConversation}
            >
              新建
            </Button>
          </div>
          <div className={styles.siderContent}>
            <Conversations
              activeKey={activeConvKey}
              items={conversations.map((c) => ({ key: c.key, label: c.label }))}
              onActiveChange={(key) => {
                setActiveConvKey(key);
                setMessages([]);
              }}
              menu={(conv) => ({
                items: [
                  {
                    key: 'delete',
                    label: '删除',
                    icon: <DeleteOutlined />,
                    danger: true,
                  },
                ],
                onClick: ({ key }) => {
                  if (key === 'delete') handleDeleteConversation(conv.key);
                },
              })}
            />
          </div>
        </div>

        {/* 右侧聊天区 */}
        <div className={styles.chatArea}>
          <div className={styles.messageList}>
            {!hasMessages ? (
              <div className={styles.welcomeWrapper}>
                <Welcome
                  icon={
                    <Avatar
                      size={64}
                      className={styles.agentAvatar}
                      icon={<RobotOutlined style={{ fontSize: 32, color: '#fff' }} />}
                    />
                  }
                  title="Genesis Agent 智能问答"
                  description="基于 ReAct Loop 架构，支持工具调用、RAG 检索、多步推理。请向我提问！"
                  extra={
                    <Button type="primary" onClick={handleNewConversation}>
                      开始新对话
                    </Button>
                  }
                  style={{ marginBottom: 32 }}
                />
                <Prompts
                  title="你可以这样问我："
                  items={mockPrompts.map((p) => ({
                    key: p.key,
                    label: p.label,
                    icon: <span>{p.icon}</span>,
                  }))}
                  onItemClick={(item) => handleSend(item.data.label as string)}
                  wrap
                />
              </div>
            ) : (
              <Bubble.List
                items={messages.map((msg) => ({
                  key: msg.id,
                  role: msg.role,
                  content: renderContent(msg),
                  loading: msg.role === 'ai' && !msg.content && msg.streaming,
                  styles: {
                    content: {
                      maxWidth: msg.role === 'user' ? '60%' : '80%',
                      background:
                        msg.role === 'user' ? token.colorPrimaryBg : token.colorBgElevated,
                    },
                  },
                }))}
                role={{
                  ai: {
                    placement: 'start',
                    avatar: (
                      <Avatar
                        className={styles.agentAvatar}
                        icon={<RobotOutlined style={{ color: '#fff' }} />}
                      />
                    ),
                  },
                  user: {
                    placement: 'end',
                    avatar: <Avatar icon={<UserOutlined />} />,
                  },
                }}
              />
            )}
          </div>

          {/* 输入区 */}
          <div className={styles.inputArea}>
            {hasMessages && (
              <div style={{ marginBottom: 8, display: 'flex', justifyContent: 'flex-end' }}>
                <Button
                  type="text"
                  size="small"
                  icon={<ClearOutlined />}
                  onClick={() => {
                    setMessages([]);
                    message.success('对话已清空');
                  }}
                >
                  清空对话
                </Button>
              </div>
            )}
            <div style={{ marginBottom: 8, display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
              <Button
                type={liveMode ? 'primary' : 'default'}
                size="small"
                onClick={() => setLiveMode((v) => !v)}
              >
                {liveMode ? 'Live API' : 'Mock'}
              </Button>
              <Button
                size="small"
                icon={<PaperClipOutlined />}
                disabled={!liveMode || loading}
                onClick={() => fileInputRef.current?.click()}
              >
                上传附件
              </Button>
              <input
                ref={fileInputRef}
                type="file"
                multiple
                style={{ display: 'none' }}
                onChange={(e) => {
                  void handlePickFiles(e.target.files);
                  e.target.value = '';
                }}
              />
              {pendingFiles.length > 0 && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  待发送: {pendingFiles.map((f) => f.name).join(', ')}
                </Typography.Text>
              )}
            </div>
            <Sender
              placeholder="输入消息，按 Enter 发送…（Shift+Enter 换行）"
              loading={loading}
              onSubmit={handleSend}
              onCancel={handleStop}
            />
            <Typography.Text
              type="secondary"
              style={{ fontSize: 11, marginTop: 8, display: 'block', textAlign: 'center' }}
            >
              {liveMode
                ? `Live: ${API_BASE} — 选文件 → POST /v1/files → StartRun(attachments ids)`
                : 'Genesis Agent 演示环境 · 默认 Mock；切换 Live API 可走真实上传'}
            </Typography.Text>
          </div>
        </div>
      </div>

      {/* 光标动画 */}
      <style>{`
        @keyframes cursor-blink {
          0%, 100% { opacity: 1; }
          50% { opacity: 0; }
        }
      `}</style>
    </XProvider>
  );
}
