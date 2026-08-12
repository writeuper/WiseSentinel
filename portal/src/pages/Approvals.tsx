import { CheckOutlined, CloseOutlined, ReloadOutlined } from '@ant-design/icons';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Button,
  Empty,
  Form,
  Input,
  Modal,
  Space,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import { approvalDecision, listApprovals } from '@/api/client';
import type { ApprovalItem } from '@/api/types';
import PageTopbar from '@/components/PageTopbar';

const statusColor: Record<string, string> = {
  pending: 'warning',
  approved: 'success',
  rejected: 'error',
  expired: 'default',
};

export default function ApprovalsPage() {
  const [items, setItems] = useState<ApprovalItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

  const [decisionTarget, setDecisionTarget] = useState<ApprovalItem | null>(null);
  const [decisionKind, setDecisionKind] = useState<'approved' | 'rejected'>('approved');
  const [form] = Form.useForm<{ comment: string }>();
  const [submitting, setSubmitting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listApprovals(1, 50);
      setItems(data.items);
      setTotal(data.total);
    } catch (e) {
      message.error(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 10_000);
    return () => clearInterval(t);
  }, [load]);

  const handleDecision = async () => {
    if (!decisionTarget) return;
    const { comment } = await form.validateFields().catch(() => ({ comment: '' }));
    setSubmitting(true);
    try {
      await approvalDecision(decisionTarget.approval_id, decisionKind, comment || '');
      message.success(`已${decisionKind === 'approved' ? '批准' : '拒绝'}`);
      setDecisionTarget(null);
      form.resetFields();
      load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : '提交失败');
    } finally {
      setSubmitting(false);
    }
  };

  const columns = useMemo(
    () => [
      { title: '审批 ID', dataIndex: 'approval_id', key: 'approval_id' },
      { title: '任务 ID', dataIndex: 'task_id', key: 'task_id' },
      { title: '类型', dataIndex: 'approval_type', key: 'approval_type' },
      {
        title: '受控目标', key: 'target', render: (_: unknown, row: ApprovalItem) => row.target?.kind === 'vector_gc_redrive' ? (
          <Space direction="vertical" size={0}>
            <Tag color="warning">向量 GC 重驱</Tag>
            <Typography.Text code>{row.target.doc_id || '-'}</Typography.Text>
            <Typography.Text type="secondary" style={{ fontFamily: 'monospace' }}>{row.target.target_key || '-'}</Typography.Text>
          </Space>
        ) : <Typography.Text type="secondary">-</Typography.Text>,
      },
      {
        title: '状态',
        dataIndex: 'status',
        key: 'status',
        render: (s: string) => <Tag color={statusColor[s] || 'default'}>{s}</Tag>,
      },
      { title: '过期时间', dataIndex: 'expired_at', key: 'expired_at' },
      {
        title: '操作',
        key: 'action',
        render: (_: unknown, row: ApprovalItem) =>
          row.status === 'pending' ? (
            <Space>
              <Button
                type="primary"
                size="small"
                icon={<CheckOutlined />}
                onClick={() => {
                  setDecisionTarget(row);
                  setDecisionKind('approved');
                }}
              >
                批准
              </Button>
              <Button
                danger
                size="small"
                icon={<CloseOutlined />}
                onClick={() => {
                  setDecisionTarget(row);
                  setDecisionKind('rejected');
                }}
              >
                拒绝
              </Button>
            </Space>
          ) : (
            <Typography.Text type="secondary">已处理</Typography.Text>
          ),
      },
    ],
    [],
  );

  return (
    <div className="page-shell">
      <PageTopbar
        title="审批中心"
        tags={[`${total} 待处理`]}
        extra={
          <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
            刷新
          </Button>
        }
      />

      <div className="page-body">
        {items.length === 0 && !loading ? (
          <Empty description="暂无待审批任务" />
        ) : (
          <Table
            rowKey="approval_id"
            dataSource={items}
            columns={columns}
            loading={loading}
            pagination={{ total, pageSize: 50, showTotal: (t) => `共 ${t} 条` }}
          />
        )}
      </div>

      <Modal
        open={decisionTarget !== null}
        title={`${decisionKind === 'approved' ? '批准' : '拒绝'}审批 ${decisionTarget?.approval_id ?? ''}`}
        onCancel={() => {
          setDecisionTarget(null);
          form.resetFields();
        }}
        onOk={handleDecision}
        confirmLoading={submitting}
        okText={decisionKind === 'approved' ? '批准' : '拒绝'}
        okButtonProps={decisionKind === 'rejected' ? { danger: true } : {}}
        destroyOnClose
      >
		{decisionTarget?.target?.kind === 'vector_gc_redrive' && (
		  <Typography.Paragraph type="warning">
			批准后将仅重新入队此精确向量清理目标。请确认你不是申请人，并核对文档与 target key。
		  </Typography.Paragraph>
		)}
		{decisionTarget?.target?.kind === 'vector_gc_redrive' && (
		  <Typography.Paragraph>
			<Typography.Text code>{decisionTarget.target.doc_id || '-'}</Typography.Text><br />
			<Typography.Text type="secondary" style={{ fontFamily: 'monospace' }}>{decisionTarget.target.target_key || '-'}</Typography.Text>
		  </Typography.Paragraph>
		)}
        <Typography.Paragraph type="secondary">
          任务 ID: <span style={{ fontFamily: 'monospace' }}>{decisionTarget?.task_id}</span>
        </Typography.Paragraph>
        <Form form={form} layout="vertical">
          <Form.Item label="审批意见（可选）" name="comment">
            <Input.TextArea rows={4} placeholder="可附上说明，便于事后审计" maxLength={500} showCount />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
