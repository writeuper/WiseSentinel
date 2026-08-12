import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { Alert, Button, Card, Collapse, Descriptions, Empty, Result, Space, Spin, Tag, Timeline, Typography } from 'antd';
import { ApiError, getTrace } from '@/api/client';
import type { AgentTraceData, AgentTraceStep } from '@/api/types';
import { formatTime } from '@/lib/format';
import PageTopbar from '@/components/PageTopbar';

const statusColor: Record<string, string> = {
  running: 'processing',
  success: 'success',
  failed: 'error',
  error: 'error',
  empty: 'warning',
  skipped: 'default',
};

function formatDuration(ms?: number) {
  if (!ms || ms <= 0) return '0ms';
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(2)}s`;
}

function TraceSteps({ steps }: { steps: AgentTraceStep[] }) {
  if (steps.length === 0) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="该 Trace 尚未记录步骤" />;
  return (
    <Timeline
      items={steps.map((step) => ({
        key: step.id,
        color: step.status === 'error' ? 'red' : step.step_type === 'tool' ? 'blue' : 'green',
        children: (
          <Collapse
            size="small"
            items={[{
              key: step.id,
              label: (
                <Space wrap>
                  <Tag>{step.step_type}</Tag>
                  <Typography.Text style={{ fontFamily: 'monospace' }}>{step.step_name}</Typography.Text>
                  <Tag color={statusColor[step.status] || 'default'}>{step.status}</Tag>
                  <Typography.Text type="secondary">{formatDuration(step.latency_ms)}</Typography.Text>
                  {step.created_at && <Typography.Text type="secondary">{formatTime(step.created_at)}</Typography.Text>}
                </Space>
              ),
              children: (
                <div>
                  {step.input_summary && (
                    <section style={{ marginBottom: 8 }}>
                      <Typography.Text type="secondary">输入摘要：</Typography.Text>
                      <pre style={summaryStyle}>{step.input_summary}</pre>
                    </section>
                  )}
                  {step.output_summary && (
                    <section style={{ marginBottom: 8 }}>
                      <Typography.Text type="secondary">输出摘要：</Typography.Text>
                      <pre style={summaryStyle}>{step.output_summary}</pre>
                    </section>
                  )}
                  {step.error_msg && <Alert type="error" message={step.error_msg} />}
                </div>
              ),
            }]}
          />
        ),
      }))}
    />
  );
}

const summaryStyle = {
  margin: '4px 0 0',
  fontSize: 12,
  background: '#fafafa',
  padding: 8,
  maxHeight: 220,
  overflow: 'auto' as const,
  whiteSpace: 'pre-wrap' as const,
  overflowWrap: 'anywhere' as const,
};

export default function TracePage() {
  const { traceId = '' } = useParams();
  const [data, setData] = useState<AgentTraceData | null>(null);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    if (!traceId || !/^[A-Za-z0-9._-]{1,64}$/.test(traceId)) {
      setLoading(false);
      setNotFound(true);
      return () => { active = false; };
    }
    setLoading(true);
    setNotFound(false);
    setError(null);
    void getTrace(traceId)
      .then((response) => {
        if (active) setData(response);
      })
      .catch((cause: unknown) => {
        if (!active) return;
        if (cause instanceof ApiError && cause.httpStatus === 404) setNotFound(true);
        else setError('Trace 暂时无法加载，请稍后重试。');
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => { active = false; };
  }, [traceId]);

  return (
    <div className="page-shell">
      <PageTopbar title="执行 Trace" tags={['仅限已授权资源', '服务端脱敏摘要']} />
      <div style={{ padding: 24, maxWidth: 1120, width: '100%', margin: '0 auto' }}>
        {loading && <div role="status" aria-live="polite" style={{ textAlign: 'center', padding: 48 }}><Spin /> <span style={{ marginLeft: 8 }}>正在加载 Trace…</span></div>}
        {!loading && notFound && (
          <Result
            status="404"
            title="Trace 不存在或无权访问"
            subTitle="为保护租户和用户数据，系统不会区分资源不存在与无访问权限。"
            extra={<Link to="/chat"><Button type="primary">返回对话</Button></Link>}
          />
        )}
        {!loading && error && <Alert type="error" showIcon message={error} action={<Button size="small" onClick={() => window.location.reload()}>重试</Button>} />}
        {!loading && data && (
          <>
            <Card title="执行概览" style={{ marginBottom: 16 }}>
              <Descriptions size="small" column={{ xs: 1, sm: 2, md: 3 }}>
                <Descriptions.Item label="Trace ID"><Typography.Text copyable={{ text: data.trace.trace_id }} code>{data.trace.trace_id}</Typography.Text></Descriptions.Item>
                <Descriptions.Item label="Agent 类型"><Tag>{data.trace.agent_type}</Tag></Descriptions.Item>
                <Descriptions.Item label="状态"><Tag color={statusColor[data.trace.status] || 'default'}>{data.trace.status}</Tag></Descriptions.Item>
                <Descriptions.Item label="总耗时">{formatDuration(data.trace.latency_ms)}</Descriptions.Item>
                <Descriptions.Item label="开始时间">{formatTime(data.trace.started_at || '')}</Descriptions.Item>
                <Descriptions.Item label="结束时间">{formatTime(data.trace.finished_at || '')}</Descriptions.Item>
              </Descriptions>
              {data.trace.error_msg && <Alert type="error" message={data.trace.error_msg} style={{ marginTop: 16 }} />}
            </Card>
            <Card title={`步骤回放（${data.steps.length}）`} extra={<Tag color="blue">服务端脱敏摘要</Tag>}>
              <TraceSteps steps={data.steps} />
            </Card>
          </>
        )}
      </div>
    </div>
  );
}
