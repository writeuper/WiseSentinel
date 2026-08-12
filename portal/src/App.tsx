import { Component, lazy, Suspense, type ErrorInfo } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { Button, Result } from 'antd';
import { useAuth } from './context/AuthContext';
import AppLayout from './layouts/AppLayout';
import LoginPage from './pages/Login';
import type { Role } from './api/types';

// Operations pages pull in charts, tables and domain-specific clients. Keep
// them out of the login/first-layout critical path; authorization is still
// evaluated before the lazy component mounts.
const ChatPage = lazy(() => import('./pages/Chat'));
const OpsPage = lazy(() => import('./pages/Ops'));
const KnowledgePage = lazy(() => import('./pages/Knowledge'));
const FaultKnowledgePage = lazy(() => import('./pages/FaultKnowledge'));
const ApprovalsPage = lazy(() => import('./pages/Approvals'));
const AdminPage = lazy(() => import('./pages/Admin'));
const VectorGCPage = lazy(() => import('./pages/VectorGC'));
const TracePage = lazy(() => import('./pages/Trace'));

function PageLoading() {
  return <div role="status" aria-live="polite" style={{ padding: 24 }}>正在加载页面…</div>;
}

class PageLoadBoundary extends Component<{ children: React.ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(_error: Error, _info: ErrorInfo) {
    // Chunk loader errors can include URL and upstream details. Do not render
    // or persist them here; the browser and deployment telemetry retain the
    // technical signal while the user receives a stable recovery action.
  }

  render() {
    if (this.state.failed) {
      return (
        <div role="alert" style={{ padding: 24 }}>
          页面资源加载失败，可能是版本刚更新。请重新加载后重试。
          <div style={{ marginTop: 12 }}>
            <button type="button" onClick={() => window.location.reload()}>重新加载</button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

function LazyPage({ children }: { children: React.ReactNode }) {
  return <PageLoadBoundary><Suspense fallback={<PageLoading />}>{children}</Suspense></PageLoadBoundary>;
}

function PrivateRoute({ children }: { children: React.ReactNode }) {
  const { authed } = useAuth();
  if (!authed) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

// This is a defense-in-depth UX guard. The API remains the authorization
// authority and enforces the same role requirements server-side.
function RoleRoute({ roles, children }: { roles: Role[]; children: React.ReactNode }) {
  const { authed, hasRole } = useAuth();
  if (!authed) return <Navigate to="/login" replace />;
  if (hasRole(roles)) return <>{children}</>;
  return (
    <Result
      status="403"
      title="无权限访问此功能"
      subTitle="你的当前角色不具备访问该企业级运维功能的权限。"
      extra={<Button type="primary" href="/chat">返回对话</Button>}
    />
  );
}

export default function App() {
  const { authed } = useAuth();

  return (
    <Routes>
      <Route
        path="/login"
        element={authed ? <Navigate to="/chat" replace /> : <LoginPage />}
      />
      <Route
        path="/"
        element={
          <PrivateRoute>
            <AppLayout />
          </PrivateRoute>
        }
      >
        <Route index element={<Navigate to="/chat" replace />} />
        <Route path="chat" element={<LazyPage><ChatPage /></LazyPage>} />
        <Route path="traces/:traceId" element={<LazyPage><TracePage /></LazyPage>} />
        <Route
          path="ops"
          element={
            <RoleRoute roles={['operator', 'sre_admin', 'platform_admin']}>
              <LazyPage><OpsPage /></LazyPage>
            </RoleRoute>
          }
        />
        <Route path="knowledge" element={<LazyPage><KnowledgePage /></LazyPage>} />
        <Route path="fault-knowledge" element={<LazyPage><FaultKnowledgePage /></LazyPage>} />
        <Route
          path="approvals"
          element={
            <RoleRoute roles={['sre_admin', 'platform_admin']}>
              <LazyPage><ApprovalsPage /></LazyPage>
            </RoleRoute>
          }
        />
        <Route
          path="admin"
          element={
            <RoleRoute roles={['sre_admin', 'platform_admin']}>
              <LazyPage><AdminPage /></LazyPage>
            </RoleRoute>
          }
        />
        <Route
          path="vector-gc"
          element={
            <RoleRoute roles={['sre_admin', 'platform_admin']}>
              <LazyPage><VectorGCPage /></LazyPage>
            </RoleRoute>
          }
        />
      </Route>
      <Route path="*" element={<Navigate to="/chat" replace />} />
    </Routes>
  );
}
