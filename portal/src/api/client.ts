import type { ApiResponse } from './types';

const API_BASE = import.meta.env.VITE_API_BASE || '/api/v1';

export class ApiError extends Error {
  code: number;
  httpStatus: number;

  constructor(code: number, message: string, httpStatus = 400) {
    super(message);
    this.code = code;
    this.httpStatus = httpStatus;
  }
}

function getToken(): string | null {
  return localStorage.getItem('ws_token');
}

export function setToken(token: string) {
  localStorage.setItem('ws_token', token);
}

export function clearToken() {
  localStorage.removeItem('ws_token');
}

export function isAuthenticated(): boolean {
  return !!getToken();
}

async function parseResponse<T>(res: Response): Promise<T> {
  const body = (await res.json()) as ApiResponse<T>;
  if (body.code !== 0) {
    throw new ApiError(body.code, body.message, res.status);
  }
  return body.data as T;
}

export async function apiRequest<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers);
  const token = getToken();
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }
  if (!headers.has('Content-Type') && !(init.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json');
  }

  const res = await fetch(`${API_BASE}${path}`, { ...init, headers });
  return parseResponse<T>(res);
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

export async function login(username: string, password: string) {
  return apiRequest<import('./types').AuthTokenData>('/auth/token', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  });
}

// ---------------------------------------------------------------------------
// Knowledge
// ---------------------------------------------------------------------------

export async function listDocuments(page = 1, size = 20, status?: string) {
  const params = new URLSearchParams({ page: String(page), size: String(size) });
  if (status) params.set('status', status);
  return apiRequest<{ items: import('./types').DocumentItem[]; total: number }>(
    `/knowledge/documents?${params}`,
  );
}

export async function uploadDocument(file: File, visibility = 'tenant', secretLevel = 1) {
  const form = new FormData();
  form.append('file', file);
  form.append('visibility', visibility);
  form.append('secret_level', String(secretLevel));
  return apiRequest<import('./types').UploadDocumentData>('/knowledge/documents/upload', {
    method: 'POST',
    body: form,
  });
}

export async function deleteDocument(docId: string) {
  return apiRequest<{ doc_id: string; status: string }>(`/knowledge/documents/${docId}`, {
    method: 'DELETE',
  });
}

export async function getIndexTask(taskId: string) {
  return apiRequest<import('./types').IndexTaskData>(`/knowledge/index-tasks/${taskId}`);
}

export async function ping() {
  return apiRequest<Record<string, string>>('/ping');
}

// ---------------------------------------------------------------------------
// Session
// ---------------------------------------------------------------------------

export async function createSession(title?: string, agentType = 'chat') {
  return apiRequest<import('./types').CreateSessionData>('/sessions', {
    method: 'POST',
    body: JSON.stringify({ title, agent_type: agentType }),
  });
}

export async function listSessions(page = 1, size = 20) {
  const params = new URLSearchParams({ page: String(page), size: String(size) });
  return apiRequest<{ items: import('./types').SessionItem[]; total: number }>(
    `/sessions?${params}`,
  );
}

export async function getMessages(sessionId: string) {
  return apiRequest<import('./types').GetSessionMessagesData>(
    `/sessions/${sessionId}/messages`,
  );
}

// ---------------------------------------------------------------------------
// Chat
// ---------------------------------------------------------------------------

export interface ChatResponse {
  session_id: string;
  answer: string;
  citations?: import('./types').CitationItem[];
  tool_calls?: import('./types').ToolCallSummary[];
  trace_id: string;
}

/** Send a synchronous chat message. */
export async function sendChat(
  sessionId: string,
  question: string,
  options?: { enable_rag?: boolean; enable_tools?: boolean },
): Promise<ChatResponse> {
  return apiRequest<ChatResponse>('/chat', {
    method: 'POST',
    body: JSON.stringify({
      session_id: sessionId,
      question,
      options: {
        enable_rag: options?.enable_rag ?? true,
        enable_tools: options?.enable_tools ?? true,
      },
    }),
  });
}

export type StreamEventCallback = {
  onConnected?: (data: string) => void;
  onMessage?: (text: string) => void;
  onCitation?: (data: import('./types').CitationItem) => void;
  onToolStart?: (data: string) => void;
  onToolEnd?: (data: string) => void;
  onError?: (errMsg: string) => void;
  onDone?: (data: string) => void;
};

/** Send a streaming chat message via SSE. */
export function sendChatStream(
  sessionId: string,
  question: string,
  options: { enable_rag?: boolean; enable_tools?: boolean },
  callbacks: StreamEventCallback,
): AbortController {
  const controller = new AbortController();
  const token = getToken();
  const apiKey = import.meta.env.VITE_DEV_API_KEY;

  // Use POST for SSE to match the backend API
  fetch(`${API_BASE}/chat/stream`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(apiKey && !token ? { 'X-API-Key': apiKey } : {}),
    },
    body: JSON.stringify({
      session_id: sessionId,
      question,
      options: {
        enable_rag: options.enable_rag ?? true,
        enable_tools: options.enable_tools ?? true,
      },
    }),
    signal: controller.signal,
  }).then(async (resp) => {
    if (!resp.ok) {
      const text = await resp.text();
      callbacks.onError?.(`HTTP ${resp.status}: ${text}`);
      return;
    }

    const reader = resp.body?.getReader();
    if (!reader) {
      callbacks.onError?.('No response body');
      return;
    }

    const decoder = new TextDecoder();
    let buffer = '';

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split('\n');
      buffer = lines.pop() || '';

      let currentEvent = 'message';
      for (const line of lines) {
        if (line.startsWith('event: ')) {
          currentEvent = line.slice(7).trim();
        } else if (line.startsWith('data: ')) {
          const data = line.slice(6).trim();
          switch (currentEvent) {
            case 'connected':
              callbacks.onConnected?.(data);
              break;
            case 'message':
              callbacks.onMessage?.(data);
              break;
            case 'citation':
              try {
                callbacks.onCitation?.(JSON.parse(data));
              } catch { /* ignore */ }
              break;
            case 'tool_start':
              callbacks.onToolStart?.(data);
              break;
            case 'tool_end':
              callbacks.onToolEnd?.(data);
              break;
            case 'error':
              callbacks.onError?.(data);
              break;
            case 'done':
              callbacks.onDone?.(data);
              break;
          }
          currentEvent = 'message';
        }
      }
    }
  }).catch((err) => {
    if (err.name !== 'AbortError') {
      callbacks.onError?.(err.message);
    }
  });

  return controller;
}

// ---------------------------------------------------------------------------
// Ops
// ---------------------------------------------------------------------------

export async function opsAnalyze(
  query?: string,
  async = false,
  maxIterations = 20,
) {
  return apiRequest<import('./types').OpsAnalyzeData>('/ops/analyze', {
    method: 'POST',
    body: JSON.stringify({
      query: query || '',
      options: { async, max_iterations: maxIterations },
    }),
  });
}

export async function getOpsTask(taskId: string) {
  return apiRequest<import('./types').OpsTaskData>(`/ops/tasks/${taskId}`);
}

export async function listOpsTasks(
  page = 1,
  size = 30,
  status?: string,
) {
  const params = new URLSearchParams({ page: String(page), size: String(size) });
  if (status) params.set('status', status);
  return apiRequest<import('./types').ListOpsTasksData>(`/ops/tasks?${params}`);
}

export async function getCurrentUser() {
  return apiRequest<import('./types').CurrentUser>('/me');
}

// ---------------------------------------------------------------------------
// Approval
// ---------------------------------------------------------------------------

export async function listApprovals(page = 1, size = 20) {
  const params = new URLSearchParams({ page: String(page), size: String(size) });
  return apiRequest<{ items: import('./types').ApprovalItem[]; total: number }>(
    `/approvals?${params}`,
  );
}

export async function approvalDecision(approvalId: string, decision: string, comment = '') {
  return apiRequest<{ approval_id: string; status: string }>(
    `/approvals/${approvalId}/decision`,
    {
      method: 'POST',
      body: JSON.stringify({ decision, comment }),
    },
  );
}

// ---------------------------------------------------------------------------
// Admin
// ---------------------------------------------------------------------------

export async function listAgentConfigs(agentType?: string) {
  const params = agentType ? new URLSearchParams({ agent_type: agentType }) : '';
  return apiRequest<{ items: import('./types').AgentConfigItem[] }>(
    `/admin/agent-configs${params ? `?${params}` : ''}`,
  );
}

export async function activateAgentConfig(agentType: string, version: string) {
  return apiRequest<{ agent_type: string; version: string; is_active: boolean }>(
    `/admin/agent-configs/${version}/activate`,
    {
      method: 'PUT',
      body: JSON.stringify({ agent_type: agentType, version }),
    },
  );
}

// ---------------------------------------------------------------------------
// Type helpers
// ---------------------------------------------------------------------------

export interface GetSessionMessagesData {
  session_id: string;
  messages: import('./types').MessageItem[];
}