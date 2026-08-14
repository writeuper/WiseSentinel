import { CheckCircleOutlined, ClockCircleOutlined, ExperimentOutlined, PlayCircleOutlined, ReloadOutlined, StopOutlined } from '@ant-design/icons';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Collapse, Descriptions, Empty, Input, Select, Slider, Space, Spin, Switch, Table, Tag, Timeline, Typography, message } from 'antd';
import { opsAnalyze, getOpsTask, getTrace, listOpsTasks } from '@/api/client';
import type { AgentTraceData, AgentTraceStep, OpsAnalyzeData, OpsEvidence, OpsFaultConclusion, OpsTaskData, OpsTaskSummary, OpsTiming } from '@/api/types';
import { formatTime } from '@/lib/format';
import PageTopbar from '@/components/PageTopbar';

const statusColor: Record<string, string> = {
  pending: 'default',
  running: 'processing',
  success: 'success',
  failed: 'error',
  awaiting_approval: 'warning',
};

const POLL_MS = 3000;

export default function OpsPage() {
  const [query, setQuery] = useState('');
  const [asyncMode, setAsyncMode] = useState(true);
  const [maxIter, setMaxIter] = useState(20);
  const [submitting, setSubmitting] = useState(false);

  const [currentResult, setCurrentResult] = useState<OpsAnalyzeData | null>(null);
  const [polledTask, setPolledTask] = useState<OpsTaskData | null>(null);
  const [traceData, setTraceData] = useState<AgentTraceData | null>(null);

  const [tasks, setTasks] = useState<OpsTaskSummary[]>([]);
  const [taskTotal, setTaskTotal] = useState(0);
  const [statusFilter, setStatusFilter] = useState<string | undefined>(undefined);
  const [loadingList, setLoadingList] = useState(false);

  const loadTasks = useCallback(async () => {
    setLoadingList(true);
    try {
      const data = await listOpsTasks(1, 30, statusFilter);
      setTasks(data.items);
      setTaskTotal(data.total);
    } catch (e) {
      message.error(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoadingList(false);
    }
  }, [statusFilter]);

  useEffect(() => {
    loadTasks();
  }, [loadTasks]);

  // Auto-refresh list every 5s
  useEffect(() => {
    const t = setInterval(loadTasks, 5000);
    return () => clearInterval(t);
  }, [loadTasks]);

  const loadTrace = useCallback(async (traceId?: string) => {
    if (!traceId) {
      setTraceData(null);
      return;
    }
    try {
      const data = await getTrace(traceId);
      setTraceData(data);
    } catch {
      setTraceData(null);
    }
  }, []);

  // Poll the currently-active async task.
  useEffect(() => {
    if (!currentResult || currentResult.status === 'success' || currentResult.status === 'failed') {
      return;
    }
    const t = setInterval(async () => {
      try {
        const data = await getOpsTask(currentResult.task_id);
        setPolledTask(data);
        if (data.status === 'success' || data.status === 'failed') {
          setCurrentResult((prev) =>
            prev ? {
              ...prev,
              status: data.status,
              result: data.result || '',
              detail: data.detail || [],
              evidence: data.evidence,
              conclusion: data.conclusion,
              timing: data.timing,
            } : prev,
          );
          loadTrace(currentResult.trace_id);
          loadTasks();
        }
      } catch {
        /* ignore transient errors */
      }
    }, POLL_MS);
    return () => clearInterval(t);
  }, [currentResult, loadTasks, loadTrace]);

  const handleStart = async () => {
    setSubmitting(true);
    try {
      const data = await opsAnalyze(query || undefined, asyncMode, maxIter);
      if (asyncMode) {
        setCurrentResult({
          task_id: data.task_id,
          status: data.status,
          result: '',
          detail: [],
          trace_id: data.trace_id,
          timing: data.timing,
        });
        setPolledTask(null);
        setTraceData(null);
        message.success(`已提交异步任务 ${data.task_id}`);
      } else {
        setCurrentResult(data);
        setPolledTask(null);
        loadTrace(data.trace_id);
      }
      loadTasks();
    } catch (e) {
      message.error(e instanceof Error ? e.message : '提交失败');
    } finally {
      setSubmitting(false);
    }
  };

  const columns = useMemo(
    () => [
      {
        title: '任务 ID',
        dataIndex: 'task_id',
        key: 'task_id',
        render: (v: string) => (
          <Typography.Text style={{ fontFamily: 'monospace' }}>{v.slice(0, 18)}…</Typography.Text>
        ),
      },
      { title: '触发类型', dataIndex: 'trigger_type', key: 'trigger_type' },
      {
        title: '状态',
        dataIndex: 'status',
        key: 'status',
        render: (s: string) => <Tag color={statusColor[s] || 'default'}>{s}</Tag>,
      },
      { title: '创建时间', dataIndex: 'created_at', key: 'created_at', render: (v: string) => formatTime(v) },
      { title: '创建者', dataIndex: 'created_by', key: 'created_by' },
      {
        title: '操作',
        key: 'action',
        render: (_: unknown, row: OpsTaskSummary) => (
          <Button
            size="small"
            onClick={async () => {
              try {
                const data = await getOpsTask(row.task_id);
                setCurrentResult({
                  task_id: data.task_id,
                  status: data.status,
                  result: data.result,
                  detail: data.detail,
                  trace_id: '',
                  evidence: data.evidence,
                  conclusion: data.conclusion,
                  timing: data.timing,
                });
                setPolledTask(data);
                setTraceData(null);
              } catch (e) {
                message.error(e instanceof Error ? e.message : '查询失败');
              }
            }}
          >
            查看
          </Button>
        ),
      },
    ],
    [],
  );

  const renderActive = () => {
    if (!currentResult) {
      return <Empty description="尚未发起任务" />;
    }
    const isDone = polledTask?.status === 'success' || polledTask?.status === 'failed' || currentResult.status === 'success' || currentResult.status === 'failed';
    const status = polledTask?.status || currentResult.status;
    const result = polledTask?.result ?? currentResult.result;
    const detail = polledTask?.detail ?? currentResult.detail;
    const evidence = polledTask?.evidence ?? currentResult.evidence;
    const conclusion = polledTask?.conclusion ?? currentResult.conclusion;
    const timing = polledTask?.timing ?? currentResult.timing;

    return (
      <Card
        title={
          <Space>
            <Tag color={statusColor[status] || 'default'}>{status}</Tag>
            <Typography.Text type="secondary" style={{ fontFamily: 'monospace' }}>
              {currentResult.task_id}
            </Typography.Text>
            {!isDone && (
              <Tag icon={<ClockCircleOutlined />} color="processing">
                正在轮询…
              </Tag>
            )}
            {isDone && status === 'success' && (
              <Tag icon={<CheckCircleOutlined />} color="success">
                完成
              </Tag>
            )}
            {isDone && status === 'failed' && (
              <Tag icon={<StopOutlined />} color="error">
                失败
              </Tag>
            )}
          </Space>
        }
        extra={!isDone && <Spin size="small" />}
      >
        {conclusion ? (
          <ConclusionCard conclusion={conclusion} />
        ) : result ? (
          <Typography.Paragraph style={{ whiteSpace: 'pre-wrap' }}>
            {result}
          </Typography.Paragraph>
        ) : (
          <Typography.Text type="secondary">等待模型输出…</Typography.Text>
        )}
        {timing && <TimingCard timing={timing} />}
        {evidence && evidence.length > 0 && <EvidenceList evidence={evidence} />}
        {traceData?.steps && traceData.steps.length > 0 && <TraceStepList steps={traceData.steps} />}
        {currentResult.trace_id && !traceData && isDone && (
          <Button size="small" onClick={() => loadTrace(currentResult.trace_id)} style={{ marginTop: 12 }}>
            加载 Trace 步骤
          </Button>
        )}
        {detail && detail.length > 0 && (
          <>
            <Typography.Title level={5} style={{ marginTop: 16 }}>
              执行步骤
            </Typography.Title>
            <Timeline
              items={detail.map((s, i) => ({
                key: i,
                children: <span style={{ fontFamily: 'monospace' }}>{s}</span>,
              }))}
            />
          </>
        )}
      </Card>
    );
  };

  return (
    <div className="page-shell">
      <PageTopbar title="告警分析" tags={['Agent: Ops', 'Plan-Execute-Replan']} />

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
            aria-label="Ops 告警分析提示词"
            placeholder="可选：自定义 Ops Prompt，留空使用默认"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            style={{ marginBottom: 16 }}
          />
          <div style={{ marginBottom: 16 }}>
            <Switch aria-label="异步模式" checked={asyncMode} onChange={setAsyncMode} />{' '}
            <span style={{ marginLeft: 8 }}>异步模式</span>
          </div>
          <Typography.Text type="secondary">Max Iterations</Typography.Text>
          <Slider
            aria-label="最大 Agent 执行步数"
            value={maxIter}
            min={5}
            max={30}
            onChange={(v) => setMaxIter(v as number)}
            style={{ marginBottom: 16 }}
          />
          <Button
            type="primary"
            icon={<PlayCircleOutlined />}
            aria-label="开始告警分析"
            block
            loading={submitting}
            onClick={handleStart}
          >
            开始分析
          </Button>
        </div>

        <div style={{ flex: 1, padding: 24, overflow: 'auto' }}>
          {renderActive()}
          <Card
            title="任务列表"
            extra={
              <Space>
                <Select
                  allowClear
                  aria-label="按任务状态筛选"
                  placeholder="全部状态"
                  style={{ width: 160 }}
                  value={statusFilter}
                  onChange={(v) => setStatusFilter(v)}
                  options={[
                    { value: 'pending', label: 'pending' },
                    { value: 'running', label: 'running' },
                    { value: 'success', label: 'success' },
                    { value: 'failed', label: 'failed' },
                    { value: 'awaiting_approval', label: 'awaiting_approval' },
                  ]}
                />
                <Button aria-label="刷新 Ops 任务列表" icon={<ReloadOutlined />} onClick={loadTasks} loading={loadingList}>
                  刷新
                </Button>
              </Space>
            }
            style={{ marginTop: 16 }}
          >
            <Table
              rowKey="task_id"
              size="small"
              dataSource={tasks}
              columns={columns}
              loading={loadingList}
              pagination={{ total: taskTotal, pageSize: 30, showTotal: (t) => `共 ${t} 条` }}
            />
            {tasks.length === 0 && !loadingList && (
              <Alert
                type="info"
                message="暂无 Ops 任务，发起一个分析或等待 Prometheus Webhook 接入即可。"
                showIcon
                style={{ marginTop: 8 }}
              />
            )}
          </Card>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Timing and Trace cards
// ---------------------------------------------------------------------------

function formatDuration(ms?: number) {
  if (!ms || ms <= 0) return '0ms';
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function TimingCard({ timing }: { timing: OpsTiming }) {
  return (
    <Card size="small" title="排障耗时" style={{ marginTop: 16 }}>
      <Descriptions size="small" column={3}>
        <Descriptions.Item label="排队耗时">{formatDuration(timing.queue_duration_ms)}</Descriptions.Item>
        <Descriptions.Item label="执行耗时">{formatDuration(timing.run_duration_ms)}</Descriptions.Item>
        <Descriptions.Item label="端到端耗时">{formatDuration(timing.e2e_duration_ms)}</Descriptions.Item>
        <Descriptions.Item label="创建时间">{formatTime(timing.created_at || '')}</Descriptions.Item>
        <Descriptions.Item label="开始时间">{formatTime(timing.started_at || '')}</Descriptions.Item>
        <Descriptions.Item label="完成时间">{formatTime(timing.finished_at || '')}</Descriptions.Item>
      </Descriptions>
    </Card>
  );
}

function TraceStepList({ steps }: { steps: AgentTraceStep[] }) {
  return (
    <>
      <Typography.Title level={5} style={{ marginTop: 16 }}>
        Trace 步骤回放（{steps.length}） <Tag color="blue">服务端脱敏摘要</Tag>
      </Typography.Title>
      <Timeline
        items={steps.map((s) => ({
          key: s.id,
          color: s.status === 'error' ? 'red' : s.step_type === 'tool' ? 'blue' : 'green',
          children: (
            <Collapse
              size="small"
              items={[{
                key: s.id,
                label: (
                  <Space wrap>
                    <Tag>{s.step_type}</Tag>
                    <Typography.Text style={{ fontFamily: 'monospace' }}>{s.step_name}</Typography.Text>
                    <Tag color={s.status === 'error' ? 'error' : 'success'}>{s.status}</Tag>
                    <Typography.Text type="secondary">{formatDuration(s.latency_ms)}</Typography.Text>
                    {s.created_at && <Typography.Text type="secondary">{formatTime(s.created_at)}</Typography.Text>}
                  </Space>
                ),
                children: (
                  <div>
                    {s.input_summary && (
                      <div style={{ marginBottom: 8 }}>
                        <Typography.Text type="secondary">输入摘要：</Typography.Text>
                        <pre style={{ margin: '4px 0 0', fontSize: 12, background: '#fafafa', padding: 8, maxHeight: 160, overflow: 'auto' }}>{s.input_summary}</pre>
                      </div>
                    )}
                    {s.output_summary && (
                      <div style={{ marginBottom: 8 }}>
                        <Typography.Text type="secondary">输出摘要：</Typography.Text>
                        <pre style={{ margin: '4px 0 0', fontSize: 12, background: '#fafafa', padding: 8, maxHeight: 220, overflow: 'auto' }}>{s.output_summary}</pre>
                      </div>
                    )}
                    {s.error_msg && <Alert type="error" message={s.error_msg} />}
                  </div>
                ),
              }]}
            />
          ),
        }))}
      />
    </>
  );
}

// ---------------------------------------------------------------------------
// Structured conclusion card
// ---------------------------------------------------------------------------

const confidenceColor: Record<string, string> = {
  high: 'success',
  mid: 'processing',
  low: 'warning',
};

function ConclusionCard({ conclusion }: { conclusion: OpsFaultConclusion }) {
  const fields: { label: string; value: string }[] = [
    { label: '故障现象', value: conclusion.symptom },
    { label: '影响范围', value: conclusion.impact },
    { label: '根因判断', value: conclusion.root_cause },
    { label: '临时止血', value: conclusion.workaround },
    { label: '根治建议', value: conclusion.remediation },
  ];
  return (
    <Card
      size="small"
      style={{ marginBottom: 16, background: '#f6ffed', border: '1px solid #b7eb8f' }}
      title={
        <Space>
          <ExperimentOutlined style={{ color: '#52c41a' }} />
          <Typography.Text strong>结构化结论</Typography.Text>
          <Tag color={confidenceColor[conclusion.confidence] || 'default'}>
            置信度: {conclusion.confidence || 'mid'}
          </Tag>
          {conclusion.source && <Tag>{conclusion.source}</Tag>}
        </Space>
      }
    >
      {fields.map((f) =>
        f.value ? (
          <div key={f.label} style={{ marginBottom: 8 }}>
            <Typography.Text type="secondary" style={{ marginRight: 8 }}>
              {f.label}：
            </Typography.Text>
            <Typography.Text style={{ whiteSpace: 'pre-wrap' }}>{f.value}</Typography.Text>
          </div>
        ) : null,
      )}
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Tool evidence list
// ---------------------------------------------------------------------------

const evidenceStatusColor: Record<string, string> = {
  success: 'success',
  error: 'error',
  awaiting_approval: 'warning',
};

function EvidenceList({ evidence }: { evidence: OpsEvidence[] }) {
  return (
    <>
      <Typography.Title level={5} style={{ marginTop: 16 }}>
        工具证据链（{evidence.length}） <Tag color="blue">服务端脱敏摘要</Tag>
      </Typography.Title>
      <Collapse
        size="small"
        items={evidence.map((e, i) => ({
          key: i,
          label: (
            <Space>
              <Tag color={evidenceStatusColor[e.status] || 'default'}>{e.status}</Tag>
              <Typography.Text style={{ fontFamily: 'monospace' }}>{e.tool_name}</Typography.Text>
              {e.source ? <Tag>{e.source}</Tag> : null}
              {e.latency_ms ? (
                <Typography.Text type="secondary">{e.latency_ms}ms</Typography.Text>
              ) : null}
              {e.timestamp ? (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {e.timestamp}
                </Typography.Text>
              ) : null}
            </Space>
          ),
          children: (
            <Typography.Text type="secondary">
              内容已由服务端抑制，不在运营平面展示。
              {e.input_bytes ? ` 入参 ${e.input_bytes} bytes。` : ''}
              {e.output_bytes ? ` 出参 ${e.output_bytes} bytes。` : ''}
            </Typography.Text>
          ),
        }))}
      />
    </>
  );
}
