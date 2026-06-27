import { LockOutlined, UserOutlined } from '@ant-design/icons';
import { Button, Card, Form, Input, Typography, message } from 'antd';
import { useState } from 'react';
import { useAuth } from '@/context/AuthContext';

export default function LoginPage() {
  const { login } = useAuth();
  const [loading, setLoading] = useState(false);

  const onFinish = async (values: { username: string; password: string }) => {
    setLoading(true);
    try {
      await login(values.username, values.password);
      message.success('登录成功');
    } catch (e) {
      message.error(e instanceof Error ? e.message : '登录失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'linear-gradient(135deg, #0F172A 0%, #1E3A5F 50%, #0F172A 100%)',
      }}
    >
      <Card style={{ width: 420, borderRadius: 16 }} styles={{ body: { padding: 40 } }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 8 }}>
          <div
            style={{
              width: 44,
              height: 44,
              borderRadius: 12,
              background: 'linear-gradient(135deg, #2563EB, #06B6D4)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 22,
            }}
          >
            🛡
          </div>
          <Typography.Title level={3} style={{ margin: 0 }}>
            智哨 WiseSentinel
          </Typography.Title>
        </div>
        <Typography.Text type="secondary">智守每一声告警，哨护每一次变更</Typography.Text>

        <Form
          layout="vertical"
          style={{ marginTop: 28 }}
          onFinish={onFinish}
          initialValues={{ username: 'sre@example.com', password: 'dev123' }}
        >
          <Form.Item name="username" label="用户名" rules={[{ required: true }]}>
            <Input prefix={<UserOutlined />} placeholder="邮箱或用户名" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true }]}>
            <Input.Password prefix={<LockOutlined />} placeholder="开发密码 dev123" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block size="large" loading={loading}>
            登录
          </Button>
        </Form>
        <Typography.Text type="secondary" style={{ display: 'block', textAlign: 'center', marginTop: 20, fontSize: 12 }}>
          企业 SSO 登录（Phase 2）
        </Typography.Text>
      </Card>
    </div>
  );
}
