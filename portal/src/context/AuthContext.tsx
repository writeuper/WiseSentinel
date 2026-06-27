import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react';
import { clearToken, isAuthenticated, login as apiLogin, setToken } from '@/api/client';

interface AuthContextValue {
  authed: boolean;
  username: string;
  login: (username: string, password: string) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [authed, setAuthed] = useState(isAuthenticated());
  const [username, setUsername] = useState(() => localStorage.getItem('ws_username') || '');

  const login = useCallback(async (user: string, password: string) => {
    const data = await apiLogin(user, password);
    setToken(data.access_token);
    localStorage.setItem('ws_username', user);
    setUsername(user);
    setAuthed(true);
  }, []);

  const logout = useCallback(() => {
    clearToken();
    localStorage.removeItem('ws_username');
    setUsername('');
    setAuthed(false);
  }, []);

  const value = useMemo(
    () => ({ authed, username, login, logout }),
    [authed, username, login, logout],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
