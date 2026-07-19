import { CheckCircleOutlined, ReloadOutlined } from '@ant-design/icons';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Col,
  Empty,
  Popconfirm,
  Row,
  Select,
  Space,
  Tag,
  Typography,
  message,
} from 'antd';
import { activateAgentConfig, listAgentConfigs } from '@/api/client';
import type { AgentConfigItem } from '@/api/types';
import { formatTime } from '@/lib/format';
import PageTopbar from '@/components/PageTopbar';

export default function AdminPage() {
  const [agentType, setAgentType] = useState('chat');
  const [items, setItems] = useState<AgentConfigItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [activating, setActivating] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listAgentConfigs(agentType);
      setItems(data.items);
    } catch (e) {
      message.error(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, [agentType]);

  useEffect(() => {
    load();
  }, [load]);

  const handleActivate = async (version: string) => {
    setActivating(version);
    try {
      await activateAgentConfig(agentType, version);
      message.success(`已激活 ${agentType} ${version}`);
      load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : '激活失败');
    } finally {
      setActivating(null);
    }
  };

  const activeVersion = useMemo(() => items.find((it) => it.is_active)?.version, [items]);

  return (
    <div className="page-shell">
      <PageTopbar
        title="Agent 配置管理"
        extra={
          <Space>
            <Select
              value={agentType}
              onChange={(v) => setAgentType(v)}
              style={{ width: 160 }}
              options={[
                { value: 'chat', label: 'Chat Agent' },
                { value: 'ops', label: 'Ops Agent' },
                { value: 'knowledge', label: 'Knowledge Agent' },
              ]}
            />
            <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
              刷新
            </Button>
          </Space>
        }
      />

      <div className="page-body">
        {activeVersion && (
          <Alert
            type="info"
            showIcon
            message={`当前 ${agentType} Agent 激活版本：${activeVersion}`}
            style={{ marginBottom: 16 }}
          />
        )}
        {items.length === 0 && !loading ? (
          <Empty description={`暂无 ${agentType} Agent 配置`} />
        ) : (
          <Row gutter={[16, 16]}>
            {items.map((it) => (
              <Col xs={24} sm={12} lg={8} key={`${it.agent_type}-${it.version}`}>
                <Card
                  title={`${it.agent_type} · ${it.version}`}
                  extra={
                    it.is_active ? (
                      <Tag icon={<CheckCircleOutlined />} color="success">
                        active
                      </Tag>
                    ) : (
                      <Tag>draft</Tag>
                    )
                  }
                  style={it.is_active ? { borderColor: '#2563EB' } : undefined}
                >
                  <Typography.Paragraph type="secondary">
                    创建时间：{formatTime(it.created_at)}
                  </Typography.Paragraph>
                  {it.is_active ? (
                    <Button type="default" disabled block icon={<CheckCircleOutlined />}>
                      已激活
                    </Button>
                  ) : (
                    <Popconfirm
                      title={`激活 ${it.version}？`}
                      description="当前激活版本会被切换。"
                      onConfirm={() => handleActivate(it.version)}
                    >
                      <Button type="primary" block loading={activating === it.version}>
                        激活
                      </Button>
                    </Popconfirm>
                  )}
                </Card>
              </Col>
            ))}
          </Row>
        )}
      </div>
    </div>
  );
}
