import { ReloadOutlined, SafetyCertificateOutlined } from '@ant-design/icons';
import { App, Button, Card, Form, Input, Modal, Select, Space, Table, Tag, Typography } from 'antd';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { listVectorGCTasks, requestVectorGCRedrive } from '@/api/client';
import type { VectorGCTaskItem, VectorGCTaskStatus } from '@/api/types';
import { formatTime } from '@/lib/format';
import PageTopbar from '@/components/PageTopbar';

const statusColor: Record<VectorGCTaskStatus, string> = {
  pending: 'default',
  running: 'processing',
  retry_wait: 'warning',
  succeeded: 'success',
  skipped: 'default',
  dead: 'error',
};

export default function VectorGCPage() {
  const { message } = App.useApp();
  const [items, setItems] = useState<VectorGCTaskItem[]>([]);
  const [total, setTotal] = useState(0);
  const [status, setStatus] = useState<VectorGCTaskStatus | undefined>('dead');
  const [loading, setLoading] = useState(false);
  const [target, setTarget] = useState<VectorGCTaskItem | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [form] = Form.useForm<{ reason: string }>();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listVectorGCTasks(1, 50, status);
      setItems(data.items || []);
      setTotal(data.total || 0);
    } catch (e) {
      message.error(e instanceof Error ? e.message : '加载向量清理任务失败');
    } finally {
      setLoading(false);
    }
  }, [message, status]);

  useEffect(() => { load(); }, [load]);

  const submitRedrive = async () => {
    if (!target) return;
    const values = await form.validateFields();
    setSubmitting(true);
    try {
      const result = await requestVectorGCRedrive(target.doc_id, target.target_key, values.reason);
      message.success(`已创建审批 ${result.approval_id}，需由另一位管理员批准后才会重新入队。`);
      setTarget(null);
      form.resetFields();
      load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : '创建重驱审批失败');
    } finally {
      setSubmitting(false);
    }
  };

  const columns = useMemo(() => [
    {
      title: '目标', key: 'target', render: (_: unknown, row: VectorGCTaskItem) => (
        <Space direction="vertical" size={0}>
          <Typography.Text code>{row.doc_id}</Typography.Text>
          <Typography.Text type="secondary" style={{ fontFamily: 'monospace' }}>{row.target_key}</Typography.Text>
        </Space>
      ),
    },
    { title: '类型', dataIndex: 'target_kind', key: 'target_kind', render: (v: string) => <Tag>{v}</Tag> },
    {
      title: '状态', dataIndex: 'status', key: 'status', render: (v: VectorGCTaskStatus) => (
        <Tag color={statusColor[v]}>{v}</Tag>
      ),
    },
    { title: '尝试', key: 'attempts', render: (_: unknown, row: VectorGCTaskItem) => `${row.attempt_count} / ${row.max_attempts}` },
    { title: '下次重试', dataIndex: 'next_attempt_at', key: 'next_attempt_at', render: (v?: string) => v ? formatTime(v) : '-' },
    {
      title: '错误摘要', dataIndex: 'last_error', key: 'last_error', ellipsis: true,
      render: (v?: string) => <Typography.Text type="secondary">{v || '-'}</Typography.Text>,
    },
    {
      title: '操作', key: 'action', render: (_: unknown, row: VectorGCTaskItem) => row.status === 'dead' ? (
        <Button size="small" type="primary" icon={<SafetyCertificateOutlined />} onClick={() => setTarget(row)}>申请重驱</Button>
      ) : <Typography.Text type="secondary">无需人工操作</Typography.Text>,
    },
  ], []);

  return (
    <div className="page-shell">
      <PageTopbar
        title="向量清理运维"
        tags={['RAG GC', `${total} 条`, '审批重驱']}
        extra={<Space>
          <Select
            allowClear value={status} placeholder="全部状态" style={{ width: 150 }} onChange={setStatus}
            options={Object.keys(statusColor).map((value) => ({ value, label: value }))}
          />
          <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
        </Space>}
      />
      <div className="page-body">
        <Card style={{ marginBottom: 16 }}>
          <Typography.Text type="secondary">
            此处只展示后端脱敏后的任务诊断。死信任务不会自动重试；申请后必须由另一位管理员在审批中心批准，系统才会重新执行 tenant/doc/target 精确的清理任务。
          </Typography.Text>
        </Card>
        <Table
          rowKey={(row) => `${row.doc_id}:${row.target_key}`}
          columns={columns}
          dataSource={items}
          loading={loading}
          scroll={{ x: 920 }}
          pagination={{ total, pageSize: 50, showTotal: (value) => `共 ${value} 条` }}
        />
      </div>
      <Modal
        open={target !== null}
        title="申请重驱死信向量清理"
        okText="提交审批"
        cancelText="取消"
        confirmLoading={submitting}
        onCancel={() => { setTarget(null); form.resetFields(); }}
        onOk={submitRedrive}
        destroyOnClose
      >
        <Typography.Paragraph type="warning">
          该操作会在批准后重新执行外部向量删除。系统会要求另一位管理员批准，且只会处理当前选定目标。
        </Typography.Paragraph>
        <Typography.Paragraph>
          <Typography.Text strong>目标：</Typography.Text><br />
          <Typography.Text code>{target?.doc_id}</Typography.Text><br />
          <Typography.Text type="secondary" style={{ fontFamily: 'monospace' }}>{target?.target_key}</Typography.Text>
        </Typography.Paragraph>
        <Form form={form} layout="vertical">
          <Form.Item name="reason" label="重驱原因" rules={[{ required: true, message: '请填写重驱原因' }]}>
            <Input.TextArea rows={4} maxLength={500} showCount placeholder="例如：Milvus 已恢复，申请重试历史清理任务" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
