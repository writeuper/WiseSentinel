import { CheckOutlined, CloseOutlined, LikeOutlined, ReloadOutlined, StopOutlined } from '@ant-design/icons';
import { App, Button, Card, Collapse, Select, Space, Table, Tag, Typography } from 'antd';
import { useCallback, useEffect, useState } from 'react';
import { approveFaultKnowledge, feedbackFaultKnowledge, listFaultKnowledge, rejectFaultKnowledge } from '@/api/client';
import type { FaultKnowledgeItem } from '@/api/types';
import { formatTime } from '@/lib/format';
import PageTopbar from '@/components/PageTopbar';

const statusColor: Record<string, string> = {
  draft: 'warning',
  approved: 'success',
  rejected: 'error',
  archived: 'default',
};

export default function FaultKnowledgePage() {
  const { message } = App.useApp();
  const [items, setItems] = useState<FaultKnowledgeItem[]>([]);
  const [total, setTotal] = useState(0);
  const [status, setStatus] = useState<string | undefined>('draft');
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listFaultKnowledge(1, 30, status);
      setItems(data.items || []);
      setTotal(data.total || 0);
    } catch (e) {
      message.error(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, [message, status]);

  useEffect(() => {
    load();
  }, [load]);

  const doApprove = async (cardId: string) => {
    try {
      const res = await approveFaultKnowledge(cardId);
      message.success(`已审核通过，索引任务：${res.task_id}`);
      load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : '审核失败');
    }
  };

  const doReject = async (cardId: string) => {
    try {
      await rejectFaultKnowledge(cardId);
      message.success('已驳回');
      load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : '驳回失败');
    }
  };

  const doFeedback = async (cardId: string, rating: 'useful' | 'bad') => {
    try {
      await feedbackFaultKnowledge(cardId, rating);
      message.success('反馈已记录');
      load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : '反馈失败');
    }
  };

  const columns = [
    {
      title: '知识卡片',
      dataIndex: 'title',
      key: 'title',
      render: (_: string, row: FaultKnowledgeItem) => (
        <div>
          <Typography.Text strong>{row.title || row.card_id}</Typography.Text>
          <div style={{ marginTop: 4 }}>
            <Space wrap>
              <Tag color={statusColor[row.status] || 'default'}>{row.status}</Tag>
              {row.service && <Tag>{row.service}</Tag>}
              {row.version && <Tag>{row.version}</Tag>}
              <Typography.Text type="secondary">权重 {row.weight.toFixed(2)}</Typography.Text>
            </Space>
          </div>
        </div>
      ),
    },
    { title: '来源任务', dataIndex: 'task_id', key: 'task_id', render: (v: string) => <Typography.Text style={{ fontFamily: 'monospace' }}>{v.slice(0, 16)}…</Typography.Text> },
    { title: '反馈', key: 'feedback', render: (_: unknown, row: FaultKnowledgeItem) => `${row.useful_count} / ${row.bad_count}` },
    { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', render: (v: string) => formatTime(v) },
    {
      title: '操作',
      key: 'action',
      render: (_: unknown, row: FaultKnowledgeItem) => (
        <Space wrap>
          {row.status === 'draft' && (
            <>
              <Button size="small" type="primary" icon={<CheckOutlined />} onClick={() => doApprove(row.card_id)}>通过</Button>
              <Button size="small" danger icon={<CloseOutlined />} onClick={() => doReject(row.card_id)}>驳回</Button>
            </>
          )}
          {row.status === 'approved' && (
            <>
              <Button size="small" icon={<LikeOutlined />} onClick={() => doFeedback(row.card_id, 'useful')}>有用</Button>
              <Button size="small" icon={<StopOutlined />} onClick={() => doFeedback(row.card_id, 'bad')}>误导</Button>
            </>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div className="page-shell">
      <PageTopbar
        title="故障知识审核"
        tags={['Phase 2', '排障即沉淀', 'Fault Case RAG']}
        extra={
          <Space>
            <Select
              allowClear
              style={{ width: 150 }}
              value={status}
              onChange={(v) => setStatus(v)}
              options={[
                { value: 'draft', label: '待审核' },
                { value: 'approved', label: '已通过' },
                { value: 'rejected', label: '已驳回' },
              ]}
            />
            <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
          </Space>
        }
      />
      <div className="page-body">
        <Card style={{ marginBottom: 16 }}>
          <Typography.Text type="secondary">
            Ops Agent 成功完成排障后会自动生成故障知识草稿；审核通过后会写入知识库并以 fault_case 分层进入 RAG 检索。
          </Typography.Text>
        </Card>
        <Table
          rowKey="card_id"
          columns={columns}
          dataSource={items}
          loading={loading}
          pagination={{ total, pageSize: 30, showTotal: (t) => `共 ${t} 条` }}
          expandable={{
            expandedRowRender: (row) => (
              <Collapse
                size="small"
                items={[
                  { key: 'symptom', label: '故障现象', children: <Typography.Paragraph>{row.symptom || '-'}</Typography.Paragraph> },
                  { key: 'impact', label: '影响范围', children: <Typography.Paragraph>{row.impact || '-'}</Typography.Paragraph> },
                  { key: 'root', label: '根因判断', children: <Typography.Paragraph>{row.root_cause || '-'}</Typography.Paragraph> },
                  { key: 'workaround', label: '临时止血', children: <Typography.Paragraph>{row.workaround || '-'}</Typography.Paragraph> },
                  { key: 'remediation', label: '根治建议', children: <Typography.Paragraph>{row.remediation || '-'}</Typography.Paragraph> },
                ]}
              />
            ),
          }}
        />
      </div>
    </div>
  );
}
