import {
  AlertOutlined,
  BookOutlined,
  MessageOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
} from '@ant-design/icons';
import { Badge, Button, Drawer, Layout, Menu, Typography } from 'antd';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useAuth } from '@/context/AuthContext';
import { sidebarTheme } from '@/theme/tokens';
import { listSessions } from '@/api/client';
import type { SessionItem } from '@/api/types';
import './AppLayout.css';

const { Sider, Content } = Layout;

const navItems = [
  { key: '/chat', icon: <MessageOutlined />, label: '对话' },
  { key: '/ops', icon: <AlertOutlined />, label: '告警分析' },
  { key: '/knowledge', icon: <BookOutlined />, label: '知识库' },
  { key: '/approvals', icon: <SafetyCertificateOutlined />, label: '审批中心', badge: 3 },
  { key: '/admin', icon: <SettingOutlined />, label: '管理' },
];

function SidebarContent({ collapsed }: { collapsed?: boolean }) {
  const navigate = useNavigate();
  const location = useLocation();
  const { username, tenantId, roles, hasRole, logout } = useAuth();
  const [sessions, setSessions] = useState<SessionItem[]>([]);

  const loadSessions = useCallback(async () => {
    try {
      const data = await listSessions(1, 10);
      setSessions(data.items || []);
    } catch {
      // Ignore - sessions are optional
    }
  }, []);

  useEffect(() => {
    if (location.pathname === '/chat') {
      loadSessions();
    }
  }, [location.pathname, loadSessions]);

  const handleNewChat = () => {
    navigate('/chat');
  };

  const handleSessionClick = (sessionId: string) => {
    // Navigate to chat with session_id in query params
    navigate(`/chat?session=${sessionId}`);
  };

  const visibleNav = useMemo(
    () =>
      navItems.filter((item) => {
        switch (item.key) {
          case '/ops':
            return hasRole(['operator', 'sre_admin', 'platform_admin']);
          case '/approvals':
            return hasRole(['sre_admin', 'platform_admin']);
          case '/admin':
            return hasRole(['sre_admin', 'platform_admin']);
          case '/knowledge':
            return true; // all authenticated users can browse
          case '/chat':
          default:
            return true;
        }
      }),
    [hasRole],
  );

  return (
    <div className="sidebar-inner">
      <div className="sidebar-brand">
        <span className="brand-icon">🛡</span>
        {!collapsed && <span className="brand-text">智哨</span>}
      </div>

      <Button
        type="default"
        icon={<PlusOutlined />}
        className="new-chat-btn"
        block={!collapsed}
        onClick={handleNewChat}
      >
        {!collapsed && '新对话'}
      </Button>

      <Menu
        theme="dark"
        mode="inline"
        selectedKeys={[location.pathname]}
        items={visibleNav.map((item) => ({
          key: item.key,
          icon: item.icon,
          label: item.badge ? (
            <span className="nav-label-with-badge">
              {item.label}
              <Badge count={item.badge} size="small" />
            </span>
          ) : (
            item.label
          ),
        }))}
        onClick={({ key }) => {
          if (key !== location.pathname) {
            navigate(key);
          }
        }}
        className="sidebar-menu"
      />

      {!collapsed && sessions.length > 0 && (
        <div className="session-list">
          <Typography.Text type="secondary" className="session-label">
            最近会话
          </Typography.Text>
          {sessions.slice(0, 10).map((s) => (
            <div
              key={s.session_id}
              className="session-item"
              onClick={() => handleSessionClick(s.session_id)}
            >
              {s.title}
            </div>
          ))}
        </div>
      )}

      <div className="sidebar-footer" onClick={logout} title="点击退出">
        <div className="avatar">{username.charAt(0).toUpperCase() || 'U'}</div>
        {!collapsed && (
          <div>
            <div className="footer-name">{username || '用户'}</div>
            <div className="footer-role">
              {roles.join(' · ') || 'operator'} · {tenantId}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

export default function AppLayout() {
  const [mobileOpen, setMobileOpen] = useState(false);
  const isMobile = useMemo(() => window.innerWidth <= 768, []);

  return (
    <Layout className="app-layout" style={{ minHeight: '100vh' }}>
      <Sider
        width={260}
        className="app-sider hide-mobile"
        style={{ background: sidebarTheme.dark.bg }}
        breakpoint="lg"
        collapsedWidth={0}
      >
        <SidebarContent />
      </Sider>

      <Drawer
        placement="left"
        open={mobileOpen}
        onClose={() => setMobileOpen(false)}
        width={260}
        styles={{ body: { padding: 0, background: sidebarTheme.dark.bg } }}
        className="mobile-drawer"
      >
        <SidebarContent />
      </Drawer>

      <Layout>
        <Content className="app-content">
          <Outlet context={{ openMobileNav: () => setMobileOpen(true), isMobile }} />
        </Content>
      </Layout>
    </Layout>
  );
}