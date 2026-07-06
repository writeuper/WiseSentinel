import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { clearToken, getCurrentUser, isAuthenticated, login as apiLogin, setToken } from '@/api/client';
import type { Role } from '@/api/types';

interface AuthContextValue {
  authed: boolean;
  username: string;
  tenantId: string;
  roles: Role[];
  hasRole: (required: Role[]) => boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => void;
  refresh: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [authed, setAuthed] = useState(isAuthenticated());
  const [username, setUsername] = useState(() => localStorage.getItem('ws_username') || '');
  const [tenantId, setTenantId] = useState(() => localStorage.getItem('ws_tenant') || 'default');
  const [roles, setRoles] = useState<Role[]>(() => {
    try {
      const raw = localStorage.getItem('ws_roles');
      if (!raw) return ['operator'];
      const parsed = JSON.parse(raw);
      if (!Array.isArray(parsed)) return ['operator'];
      return parsed as Role[];
    } catch {
      return ['operator'];
    }
  });

  // Fetch identity from /me on mount to refresh roles / tenant.
  useEffect(() => {
    if (!authed) return;
    let cancelled = false;
    (async () => {
      try {
        const data = await getCurrentUser();
        if (cancelled) return;
        if (data.roles && data.roles.length > 0) {
          setRoles(data.roles as Role[]);
          localStorage.setItem('ws_roles', JSON.stringify(data.roles));
        }
        if (data.tenant_id) {
          setTenantId(data.tenant_id);
          localStorage.setItem('ws_tenant', data.tenant_id);
        }
        if (data.username) {
          setUsername(data.username);
          localStorage.setItem('ws_username', data.username);
        }
      } catch {
        /* keep the cached roles if /me fails (e.g., expired token) */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [authed]);

  const login = useCallback(async (user: string, password: string) => {
    const data = await apiLogin(user, password);
    setToken(data.access_token);
    localStorage.setItem('ws_username', user);
    setUsername(user);
    setAuthed(true);
    try {
      const me = await getCurrentUser();
      if (me.roles && me.roles.length > 0) {
        setRoles(me.roles as Role[]);
        localStorage.setItem('ws_roles', JSON.stringify(me.roles));
      }
      if (me.tenant_id) {
        setTenantId(me.tenant_id);
        localStorage.setItem('ws_tenant', me.tenant_id);
      }
    } catch {
      /* ignore */
    }
  }, []);

  const logout = useCallback(() => {
    clearToken();
    localStorage.removeItem('ws_username');
    localStorage.removeItem('ws_tenant');
    localStorage.removeItem('ws_roles');
    setUsername('');
    setTenantId('default');
    setRoles(['operator']);
    setAuthed(false);
  }, []);

  const refresh = useCallback(async () => {
    if (!authed) return;
    try {
      const me = await getCurrentUser();
      if (me.roles && me.roles.length > 0) {
        setRoles(me.roles as Role[]);
        localStorage.setItem('ws_roles', JSON.stringify(me.roles));
      }
      if (me.tenant_id) {
        setTenantId(me.tenant_id);
        localStorage.setItem('ws_tenant', me.tenant_id);
      }
    } catch {
      /* ignore */
    }
  }, [authed]);

  const hasRole = useCallback(
    (required: Role[]) => {
      if (required.length === 0) return true;
      const set = new Set(roles);
      return required.some((r) => set.has(r));
    },
    [roles],
  );

  const value = useMemo(
    () => ({ authed, username, tenantId, roles, hasRole, login, logout, refresh }),
    [authed, username, tenantId, roles, hasRole, login, logout, refresh],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
