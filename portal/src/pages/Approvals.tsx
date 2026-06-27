import { Button, Table, Tag } from 'antd';
import PageTopbar from '@/components/PageTopbar';

const mockData = [
  { approval_id: 'ap_001', task_id: 'ops_task_8c2f', approval_type: 'tool_invoke', status: 'pending', expired_at: '2h 后' },
  { approval_id: 'ap_002', task_id: 'ops_task_1a9b', approval_type: 'ops_remediation', status: 'pending', expired_at: '5h 后' },
];

export default function ApprovalsPage() {
  return (
    <div className="page-shell">
      <PageTopbar title="审批中心" tags={['3 待处理']} />

      <div className="page-body">
        <Table
          rowKey="approval_id"
          dataSource={mockData}
          columns={[
            { title: '类型', dataIndex: 'approval_type' },
            { title: '关联任务', dataIndex: 'task_id' },
            {
              title: '状态',
              dataIndex: 'status',
              render: (s: string) => <Tag color="warning">{s}</Tag>,
            },
            { title: '过期时间', dataIndex: 'expired_at' },
            {
              title: '操作',
              render: () => (
                <>
                  <Button type="primary" size="small" style={{ marginRight: 8 }} disabled>
                    批准
                  </Button>
                  <Button danger size="small" disabled>
                    拒绝
                  </Button>
                </>
              ),
            },
          ]}
        />
      </div>
    </div>
  );
}
