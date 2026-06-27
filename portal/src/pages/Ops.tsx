import { PlayCircleOutlined } from '@ant-design/icons';
import { Button, Card, Input, Slider, Switch, Tag, Timeline, Typography } from 'antd';
import PageTopbar from '@/components/PageTopbar';

export default function OpsPage() {
  return (
    <div className="page-shell">
      <PageTopbar title="告警分析" tags={['Agent: Ops', 'Plan-Execute']} />

      <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
        <div
          style={{
            width: 360,
            borderRight: '1px solid #e2e8f0',
            padding: 24,
            background: '#fff',
            flexShrink: 0,
          }}
          className="hide-mobile"
        >
          <Typography.Title level={5}>分析任务</Typography.Title>
          <Input.TextArea
            rows={5}
            placeholder="可选：自定义 Ops Prompt，留空使用默认"
            style={{ marginBottom: 16 }}
            disabled
          />
          <div style={{ marginBottom: 16 }}>
            <Switch disabled /> <span style={{ marginLeft: 8 }}>异步模式</span>
          </div>
          <Typography.Text type="secondary">Max Iterations</Typography.Text>
          <Slider defaultValue={20} min={5} max={30} disabled style={{ marginBottom: 16 }} />
          <Button type="primary" icon={<PlayCircleOutlined />} block disabled>
            开始分析（M4 接入）
          </Button>
        </div>

        <div style={{ flex: 1, padding: 24, overflow: 'auto' }}>
          <Card style={{ marginBottom: 16 }}>
            <Tag color="success">success</Tag>
            <Tag style={{ marginLeft: 8, fontFamily: 'monospace' }}>
              task: ops_7f3a…（演示数据）
            </Tag>
            <Typography.Title level={4} style={{ marginTop: 16 }}>
              告警分析报告
            </Typography.Title>
            <Typography.Text type="secondary">
              检测到 2 条 firing 告警，已关联 Runbook 并完成根因初判。
            </Typography.Text>
          </Card>

          <Card title="执行步骤">
            <Timeline
              items={[
                { children: '调用 query_prometheus_alerts → payment-service 实例下线' },
                { children: '调用 query_internal_docs → 匹配「服务下线告警处理手册」' },
                { children: '调用 query_logs → Connection refused 错误' },
              ]}
            />
          </Card>
        </div>
      </div>
    </div>
  );
}
