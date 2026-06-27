import { Navigate, Route, Routes } from 'react-router-dom';
import { useAuth } from './context/AuthContext';
import AppLayout from './layouts/AppLayout';
import AdminPage from './pages/Admin';
import ApprovalsPage from './pages/Approvals';
import ChatPage from './pages/Chat';
import KnowledgePage from './pages/Knowledge';
import LoginPage from './pages/Login';
import OpsPage from './pages/Ops';

function PrivateRoute({ children }: { children: React.ReactNode }) {
  const { authed } = useAuth();
  if (!authed) return <Navigate to="/login" replace />;
  return <>{children}</>;
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
        <Route path="chat" element={<ChatPage />} />
        <Route path="ops" element={<OpsPage />} />
        <Route path="knowledge" element={<KnowledgePage />} />
        <Route path="approvals" element={<ApprovalsPage />} />
        <Route path="admin" element={<AdminPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/chat" replace />} />
    </Routes>
  );
}
