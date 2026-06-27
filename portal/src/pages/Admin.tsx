import { Button, Card, Col, Row, Tag } from 'antd';
import PageTopbar from '@/components/PageTopbar';

const configs = [
  { agent: 'chat', version: 'v1', active: true, desc: 'RAG + ReAct · 4 tools' },
  { agent: 'chat', version: 'v2-beta', active: false, desc: '优化 Prompt · 6 tools' },
  { agent: 'ops', version: 'v1', active: true, desc: 'Plan-Execute · max_iter=20' },
];

export default function AdminPage() {
  return (
    <div className="page-shell">
      <PageTopbar title="Agent 配置管理" />

      <div className="page-body">
        <Row gutter={[16, 16]}>
          {configs.map((c) => (
            <Col xs={24} sm={12} lg={8} key={`${c.agent}-${c.version}`}>
              <Card
                title={`${c.agent} · ${c.version}`}
                extra={c.active ? <Tag color="success">active</Tag> : <Tag>draft</Tag>}
                style={c.active ? { borderColor: '#2563EB' } : undefined}
              >
                <p style={{ color: '#64748b', fontSize: 13 }}>{c.desc}</p>
                <Button type={c.active ? 'default' : 'primary'} disabled={c.active} block>
                  {c.active ? '已激活' : '激活（M5+）'}
                </Button>
              </Card>
            </Col>
          ))}
        </Row>
      </div>
    </div>
  );
}
