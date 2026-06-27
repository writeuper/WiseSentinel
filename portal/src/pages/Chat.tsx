import { SendOutlined } from '@ant-design/icons';
import { Button, Checkbox, Input, Tag, Typography } from 'antd';
import { useState } from 'react';
import PageTopbar from '@/components/PageTopbar';

const promptChips = [
  '服务下线告警怎么处理？',
  '帮我查一下当前 firing 的告警',
  'Pod CrashLoopBackOff 怎么排查？',
];

export default function ChatPage() {
  const [showDemo, setShowDemo] = useState(false);

  return (
    <div className="page-shell">
      <PageTopbar
        title={showDemo ? '服务下线告警怎么处理' : '新对话'}
        tags={showDemo ? ['Agent: Chat', 'trace: —'] : ['Agent: Chat']}
      />

      <div className="chat-scroll">
        <div className="chat-inner">
          {!showDemo ? (
            <div style={{ textAlign: 'center', padding: '60px 20px' }}>
              <Typography.Title level={3}>有什么可以帮您？</Typography.Title>
              <Typography.Paragraph type="secondary">
                智哨可检索 Runbook、查询告警与日志，辅助 OnCall 诊断
              </Typography.Paragraph>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10, justifyContent: 'center', marginTop: 24 }}>
                {promptChips.map((p) => (
                  <Button key={p} shape="round" onClick={() => setShowDemo(true)}>
                    {p}
                  </Button>
                ))}
              </div>
              <Tag color="orange" style={{ marginTop: 24 }}>
                M3 里程碑：Chat Agent 流式对话即将接入
              </Tag>
            </div>
          ) : (
            <>
              <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 20 }}>
                <div className="user-bubble">服务下线告警怎么处理？我们生产环境 payment-service 刚刚触发了告警。</div>
              </div>
              <div className="ai-bubble">
                <Typography.Paragraph>
                  根据内部 Runbook 和当前告警上下文，建议按以下步骤处理：
                </Typography.Paragraph>
                <ol style={{ paddingLeft: 20 }}>
                  <li>确认 Alertmanager 中 firing 告警详情</li>
                  <li>检查实例健康检查与最近变更</li>
                  <li>查看应用日志中的 ERROR 关键字</li>
                </ol>
                <div className="citation-card">
                  <strong style={{ color: '#166534' }}>📄 告警处理手册.md</strong>
                  <div>服务不可用，监控显示实例下线。处理步骤：登录控制台确认实例状态…</div>
                </div>
                <div className="tool-call-bar">🔧 query_internal_docs <Tag color="success">✓ 1.2s</Tag></div>
                <div className="tool-call-bar">🔧 query_prometheus_alerts <Tag color="success">✓ 0.8s</Tag></div>
              </div>
            </>
          )}
        </div>
      </div>

      <div style={{ padding: '16px 24px 24px', display: 'flex', justifyContent: 'center' }}>
        <div
          style={{
            width: '100%',
            maxWidth: 768,
            background: '#fff',
            border: '1px solid #e2e8f0',
            borderRadius: 12,
            padding: '12px 16px',
          }}
        >
          <Input.TextArea
            placeholder="输入问题，Enter 发送…"
            autoSize={{ minRows: 2, maxRows: 6 }}
            disabled
          />
          <div
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              marginTop: 10,
              paddingTop: 10,
              borderTop: '1px solid #e2e8f0',
            }}
          >
            <div style={{ display: 'flex', gap: 16, fontSize: 12, color: '#64748b' }}>
              <Checkbox defaultChecked disabled>
                RAG 知识库
              </Checkbox>
              <Checkbox defaultChecked disabled>
                工具调用
              </Checkbox>
            </div>
            <Button type="primary" icon={<SendOutlined />} disabled>
              发送
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
