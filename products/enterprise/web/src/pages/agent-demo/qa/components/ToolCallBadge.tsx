/**
 * 工具调用状态展示组件
 * 模拟 Agent 执行工具调用时的中间状态展示
 */
import { Tag, Space, Tooltip } from 'antd';
import {
  CodeOutlined,
  SearchOutlined,
  ApiOutlined,
  CheckCircleOutlined,
  LoadingOutlined,
} from '@ant-design/icons';

export interface ToolCallRecord {
  id: string;
  name: string;
  type: 'code_exec' | 'search' | 'api_call' | 'rag_search';
  status: 'calling' | 'done' | 'error';
  input?: string;
  output?: string;
  duration?: number;
}

const typeConfig = {
  code_exec: { icon: <CodeOutlined />, label: '代码执行', color: 'purple' },
  search: { icon: <SearchOutlined />, label: '网络搜索', color: 'blue' },
  api_call: { icon: <ApiOutlined />, label: 'API 调用', color: 'orange' },
  rag_search: { icon: <SearchOutlined />, label: 'RAG 检索', color: 'cyan' },
};

interface ToolCallBadgeProps {
  tools: ToolCallRecord[];
}

export default function ToolCallBadge({ tools }: ToolCallBadgeProps) {
  if (!tools.length) return null;

  return (
    <Space wrap style={{ marginBottom: 8 }}>
      {tools.map((tool) => {
        const config = typeConfig[tool.type];
        const isLoading = tool.status === 'calling';
        const isDone = tool.status === 'done';

        return (
          <Tooltip
            key={tool.id}
            title={
              tool.output ? (
                <div style={{ maxWidth: 300, fontSize: 12 }}>
                  <div>
                    <strong>输入：</strong>
                    {tool.input}
                  </div>
                  <div>
                    <strong>输出：</strong>
                    {tool.output}
                  </div>
                  {tool.duration && (
                    <div>
                      <strong>耗时：</strong>
                      {tool.duration}ms
                    </div>
                  )}
                </div>
              ) : undefined
            }
          >
            <Tag
              icon={isLoading ? <LoadingOutlined /> : isDone ? <CheckCircleOutlined /> : config.icon}
              color={tool.status === 'error' ? 'error' : config.color}
              style={{ cursor: 'pointer' }}
            >
              {config.label}: {tool.name}
              {isDone && tool.duration ? ` (${tool.duration}ms)` : ''}
            </Tag>
          </Tooltip>
        );
      })}
    </Space>
  );
}
