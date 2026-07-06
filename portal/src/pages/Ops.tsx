import { CheckCircleOutlined, ClockCircleOutlined, PlayCircleOutlined, ReloadOutlined, StopOutlined } from '@ant-design/icons';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Empty, Input, Select, Slider, Space, Spin, Switch, Table, Tag, Timeline, Typography, message } from 'antd';
import { opsAnalyze, getOpsTask, listOpsTasks } from '@/api/client';
import type { OpsAnalyzeData, OpsTaskData, OpsTaskSummary } from '@/api/types';
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
            prev ? { ...prev, status: data.status, result: data.result || '', detail: data.detail || [] } : prev,
          );
          loadTasks();
        }
      } catch {
        /* ignore transient errors */
      }
    }, POLL_MS);
    return () => clearInterval(t);
  }, [currentResult, loadTasks]);

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
        });
        setPolledTask(null);
        message.success(`已提交异步任务 ${data.task_id}`);
      } else {
        setCurrentResult(data);
        setPolledTask(null);
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
      { title: '创建时间', dataIndex: 'created_at', key: 'created_at' },
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
                });
                setPolledTask(data);
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
        {result ? (
          <Typography.Paragraph style={{ whiteSpace: 'pre-wrap' }}>
            {result}
          </Typography.Paragraph>
        ) : (
          <Typography.Text type="secondary">等待模型输出…</Typography.Text>
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
            placeholder="可选：自定义 Ops Prompt，留空使用默认"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            style={{ marginBottom: 16 }}
          />
          <div style={{ marginBottom: 16 }}>
            <Switch checked={asyncMode} onChange={setAsyncMode} />{' '}
            <span style={{ marginLeft: 8 }}>异步模式</span>
          </div>
          <Typography.Text type="secondary">Max Iterations</Typography.Text>
          <Slider
            value={maxIter}
            min={5}
            max={30}
            onChange={(v) => setMaxIter(v as number)}
            style={{ marginBottom: 16 }}
          />
          <Button
            type="primary"
            icon={<PlayCircleOutlined />}
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
                <Button icon={<ReloadOutlined />} onClick={loadTasks} loading={loadingList}>
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
