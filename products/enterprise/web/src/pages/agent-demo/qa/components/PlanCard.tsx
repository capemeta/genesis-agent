/**
 * 结构化任务卡片组件
 * 模拟 Agent 输出的计划/任务卡片（对应 @ant-design/x-card 的 A2UI 协议渲染）
 * 实际接入后端后，替换为 XCard.Box + XCard.Card 即可
 */
import { Card, Checkbox, Tag, Space, Typography, Progress, Divider, Badge } from 'antd';
import {
  CheckSquareOutlined,
  ClockCircleOutlined,
  FlagOutlined,
  TeamOutlined,
} from '@ant-design/icons';

const { Text, Title, Paragraph } = Typography;

export interface TaskItem {
  id: string;
  text: string;
  done: boolean;
  assignee?: string;
}

export interface PhaseCard {
  id: string;
  title: string;
  duration: string;
  status: 'todo' | 'doing' | 'done';
  priority: 'high' | 'medium' | 'low';
  tasks: TaskItem[];
}

export interface PlanCardData {
  title: string;
  summary: string;
  phases: PhaseCard[];
}

const statusConfig = {
  todo: { color: 'default', label: '待开始' },
  doing: { color: 'processing', label: '进行中' },
  done: { color: 'success', label: '已完成' },
};

const priorityConfig = {
  high: { color: '#ff4d4f', label: '高' },
  medium: { color: '#fa8c16', label: '中' },
  low: { color: '#52c41a', label: '低' },
};

interface PlanCardProps extends PlanCardData {}

export default function PlanCard({ title, summary, phases }: PlanCardProps) {
  const totalTasks = phases.reduce((acc, p) => acc + p.tasks.length, 0);
  const doneTasks = phases.reduce((acc, p) => acc + p.tasks.filter((t) => t.done).length, 0);
  const progress = totalTasks > 0 ? Math.round((doneTasks / totalTasks) * 100) : 0;

  return (
    <div style={{ maxWidth: 600 }}>
      {/* 标题与进度 */}
      <div style={{ marginBottom: 16 }}>
        <Space align="center" style={{ marginBottom: 8 }}>
          <CheckSquareOutlined style={{ fontSize: 18, color: '#1677ff' }} />
          <Title level={4} style={{ margin: 0 }}>
            {title}
          </Title>
        </Space>
        <Paragraph type="secondary" style={{ margin: '4px 0 12px', fontSize: 13 }}>
          {summary}
        </Paragraph>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <Progress
            percent={progress}
            size="small"
            style={{ flex: 1 }}
            status={progress === 100 ? 'success' : 'active'}
          />
          <Text type="secondary" style={{ fontSize: 12, whiteSpace: 'nowrap' }}>
            {doneTasks}/{totalTasks} 任务
          </Text>
        </div>
      </div>

      {/* 各阶段卡片 */}
      <Space direction="vertical" style={{ width: '100%' }} size={10}>
        {phases.map((phase, idx) => (
          <Card
            key={phase.id}
            size="small"
            title={
              <Space size={8}>
                <Badge
                  count={idx + 1}
                  style={{ backgroundColor: '#1677ff', fontSize: 11, minWidth: 18, height: 18 }}
                />
                <Text strong style={{ fontSize: 13 }}>
                  {phase.title}
                </Text>
                <Tag color={statusConfig[phase.status].color} style={{ fontSize: 11 }}>
                  {statusConfig[phase.status].label}
                </Tag>
              </Space>
            }
            extra={
              <Space size={6}>
                <ClockCircleOutlined style={{ color: '#8c8c8c', fontSize: 12 }} />
                <Text type="secondary" style={{ fontSize: 11 }}>
                  {phase.duration}
                </Text>
                <Divider type="vertical" />
                <FlagOutlined
                  style={{ color: priorityConfig[phase.priority].color, fontSize: 12 }}
                />
                <Text style={{ fontSize: 11, color: priorityConfig[phase.priority].color }}>
                  {priorityConfig[phase.priority].label}优先
                </Text>
              </Space>
            }
            styles={{ body: { padding: '8px 16px 12px' } }}
          >
            <Space direction="vertical" style={{ width: '100%' }} size={6}>
              {phase.tasks.map((task) => (
                <div
                  key={task.id}
                  style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}
                >
                  <Checkbox
                    checked={task.done}
                    disabled
                    style={{ marginTop: 2, flexShrink: 0 }}
                  />
                  <div style={{ flex: 1 }}>
                    <Text
                      style={{
                        fontSize: 13,
                        color: task.done ? '#8c8c8c' : 'inherit',
                        textDecoration: task.done ? 'line-through' : 'none',
                      }}
                    >
                      {task.text}
                    </Text>
                    {task.assignee && (
                      <Text
                        type="secondary"
                        style={{ fontSize: 11, marginLeft: 8 }}
                      >
                        <TeamOutlined style={{ marginRight: 2 }} />
                        {task.assignee}
                      </Text>
                    )}
                  </div>
                </div>
              ))}
            </Space>
          </Card>
        ))}
      </Space>
    </div>
  );
}

/** 解析 A2UI 命令或直接返回预设的演示数据 */
export function parsePlanFromCommands(_commands: unknown[]): PlanCardData {
  return {
    title: '微服务拆分计划',
    summary: '基于 DDD 领域驱动设计，将单体服务渐进式拆分为微服务架构，预计 8 周完成。',
    phases: [
      {
        id: 'phase-1',
        title: '服务边界识别',
        duration: '第 1-2 周',
        status: 'todo',
        priority: 'high',
        tasks: [
          { id: 't1', text: '梳理业务域，识别聚合根与限界上下文', done: false, assignee: '架构师' },
          { id: 't2', text: '绘制服务依赖图，识别循环依赖', done: false, assignee: '架构师' },
          { id: 't3', text: '定义各服务的 API 契约（OpenAPI）', done: false, assignee: '后端团队' },
        ],
      },
      {
        id: 'phase-2',
        title: '数据层解耦',
        duration: '第 3-4 周',
        status: 'todo',
        priority: 'high',
        tasks: [
          { id: 't4', text: '按服务拆分数据库 Schema，消除跨服务 JOIN', done: false, assignee: 'DBA' },
          { id: 't5', text: '引入 Outbox 模式替代同步跨服务调用', done: false, assignee: '后端团队' },
          { id: 't6', text: '数据迁移脚本编写与验证', done: false, assignee: 'DBA' },
        ],
      },
      {
        id: 'phase-3',
        title: '服务拆分与部署',
        duration: '第 5-6 周',
        status: 'todo',
        priority: 'medium',
        tasks: [
          { id: 't7', text: '独立部署各服务，接入 K8s Deployment', done: false, assignee: 'DevOps' },
          { id: 't8', text: '配置服务网格（Istio）与链路追踪', done: false, assignee: 'DevOps' },
        ],
      },
      {
        id: 'phase-4',
        title: '灰度验证与上线',
        duration: '第 7-8 周',
        status: 'todo',
        priority: 'medium',
        tasks: [
          { id: 't9', text: '10% 流量灰度，监控错误率与延迟 P99', done: false, assignee: '全员' },
          { id: 't10', text: '全量切流，下线旧单体服务', done: false, assignee: '全员' },
        ],
      },
    ],
  };
}
