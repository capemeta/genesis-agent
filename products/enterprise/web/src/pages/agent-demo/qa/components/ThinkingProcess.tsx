/**
 * 思维链展示组件
 * 使用 ant-design-x 的 ThoughtChain 展示 Agent 推理过程
 */
import { ThoughtChain } from '@ant-design/x';
import { LoadingOutlined } from '@ant-design/icons';
import { Typography } from 'antd';
import type { ThoughtStep } from '../mockData';

interface ThinkingProcessProps {
  steps: ThoughtStep[];
  running?: boolean;
}

export default function ThinkingProcess({ steps, running }: ThinkingProcessProps) {
  return (
    <div style={{ padding: '4px 0 12px' }}>
      <ThoughtChain
        items={steps.map((step) => ({
          title: step.title,
          description: step.description,
          // ThoughtChain 有效状态：loading | success | error | abort
          status: step.status,
          // 每个条目支持折叠
          collapsible: !!step.description,
        }))}
      />
      {running && (
        <Typography.Text type="secondary" style={{ fontSize: 12, marginTop: 8, display: 'flex', alignItems: 'center', gap: 6 }}>
          <LoadingOutlined />
          Agent 正在思考中...
        </Typography.Text>
      )}
    </div>
  );
}
