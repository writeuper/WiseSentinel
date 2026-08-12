import type { ApiResponse } from './types';

const API_BASE = import.meta.env.VITE_API_BASE || '/api/v1';

export class ApiError extends Error {
  code: number;
  httpStatus: number;
  retryAfterSeconds: number | null;

  constructor(code: number, message: string, httpStatus = 400, retryAfterSeconds: number | null = null) {
    super(message);
    this.code = code;
    this.httpStatus = httpStatus;
    this.retryAfterSeconds = retryAfterSeconds;
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

function parseRetryAfter(value: string | null): number | null {
  if (!value || !/^\d+$/.test(value)) return null;
  const seconds = Number(value);
  return Number.isSafeInteger(seconds) && seconds > 0 && seconds <= 60 ? seconds : null;
}

async function parseResponse<T>(res: Response): Promise<T> {
  const body = (await res.json()) as ApiResponse<T>;
  if (body.code !== 0) {
    throw new ApiError(body.code, body.message, res.status, parseRetryAfter(res.headers.get('Retry-After')));
  }
  return body.data as T;
}

export function describeRequestError(error: unknown, fallback = '请求失败'): string {
  if (error instanceof ApiError && error.code === 50304 && error.retryAfterSeconds) {
    return `${error.message}，建议 ${error.retryAfterSeconds} 秒后重试。`;
  }
  return error instanceof Error && error.message ? error.message : fallback;
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

export async function reindexDocument(docId: string) {
  return apiRequest<import('./types').ReindexDocumentData>(
    `/knowledge/documents/${encodeURIComponent(docId)}/reindex`,
    { method: 'POST' },
  );
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
  idempotencyKey?: string,
): Promise<ChatResponse> {
  return apiRequest<ChatResponse>('/chat', {
    method: 'POST',
    // A key belongs to a logical user turn, not to the session or browser.
    // It is intentionally a header so it is not exposed through URLs.
    headers: idempotencyKey ? { 'Idempotency-Key': idempotencyKey } : undefined,
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
  onError?: (error: Error) => void;
  onDone?: (data: string) => void;
};

/** Send a streaming chat message. */
export function sendChatStream(
  sessionId: string,
  question: string,
  options: { enable_rag?: boolean; enable_tools?: boolean },
  callbacks: StreamEventCallback,
): AbortController {
  const controller = new AbortController();
  void consumeChatSSE(sessionId, question, options, callbacks, controller.signal);

  return controller;
}

async function consumeChatSSE(
  sessionId: string,
  question: string,
  options: { enable_rag?: boolean; enable_tools?: boolean },
  callbacks: StreamEventCallback,
  signal: AbortSignal,
): Promise<void> {
  const headers = new Headers({ 'Content-Type': 'application/json', Accept: 'text/event-stream' });
  const token = getToken();
  if (token) headers.set('Authorization', `Bearer ${token}`);
  try {
    const response = await fetch(`${API_BASE}/chat/stream`, {
      method: 'POST', headers, signal,
      body: JSON.stringify({ session_id: sessionId, question, options: {
        enable_rag: options.enable_rag ?? true,
        enable_tools: options.enable_tools ?? true,
      } }),
    });
    if (!response.ok) {
      await parseResponse(response);
      throw new Error('流式请求失败');
    }
    if (!response.body) throw new Error('流式响应不可用');

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    let seenDone = false;
    let seenError = false;
    while (!signal.aborted) {
      const next = await reader.read();
      if (next.done) break;
      buffer += decoder.decode(next.value, { stream: true }).replace(/\r\n/g, '\n');
      let separator = buffer.indexOf('\n\n');
      while (separator >= 0) {
        const event = dispatchSSEFrame(buffer.slice(0, separator), callbacks);
        seenDone = seenDone || event === 'done';
        seenError = seenError || event === 'error';
        buffer = buffer.slice(separator + 2);
        separator = buffer.indexOf('\n\n');
      }
    }
    if (!signal.aborted && buffer.trim()) {
      const event = dispatchSSEFrame(buffer, callbacks);
      seenDone = seenDone || event === 'done';
      seenError = seenError || event === 'error';
    }
    // A TCP/HTTP stream can end without an SSE terminal frame. Treating that
    // as success leaves the chat UI permanently in "sending" state and can
    // claim completion before the backend persisted the turn.
    if (!signal.aborted && !seenDone && !seenError) {
      callbacks.onError?.(new Error('流式响应意外结束，请稍后重试'));
    }
  } catch (error) {
    if (!signal.aborted) callbacks.onError?.(error instanceof Error ? error : new Error('流式请求失败'));
  }
}

function dispatchSSEFrame(frame: string, callbacks: StreamEventCallback): string {
  let event = 'message';
  const data: string[] = [];
  for (const line of frame.split('\n')) {
    if (line.startsWith('event:')) event = line.slice(6).trim();
    if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, ''));
  }
  const payload = data.join('\n');
  if (event === 'connected') callbacks.onConnected?.(payload);
  else if (event === 'message') callbacks.onMessage?.(payload);
  else if (event === 'citation') {
    try { callbacks.onCitation?.(JSON.parse(payload)); } catch { callbacks.onError?.(new Error('知识引用流格式无效')); }
  } else if (event === 'tool') {
    try {
      const tool = JSON.parse(payload) as { tool?: string };
      callbacks.onToolStart?.(tool.tool || 'tool');
      callbacks.onToolEnd?.(payload);
    } catch { callbacks.onError?.(new Error('工具事件流格式无效')); }
  } else if (event === 'error') {
    try {
      const error = JSON.parse(payload) as { code?: number; message?: string; retry_after_seconds?: number };
      const retryAfterSeconds = error.retry_after_seconds;
      if (error.code === 50304 && typeof error.message === 'string' && typeof retryAfterSeconds === 'number' && Number.isInteger(retryAfterSeconds) && retryAfterSeconds > 0 && retryAfterSeconds <= 60) {
        callbacks.onError?.(new ApiError(error.code, error.message, 503, retryAfterSeconds));
        return event;
      }
    } catch { /* legacy plain-text SSE error */ }
    callbacks.onError?.(new Error(payload || '流式请求失败'));
  }
  else if (event === 'done') callbacks.onDone?.(payload);
  return event;
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

export async function getTrace(traceId: string) {
  return apiRequest<import('./types').AgentTraceData>(`/traces/${traceId}`);
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

export async function listFaultKnowledge(page = 1, size = 20, status?: string) {
  const params = new URLSearchParams({ page: String(page), size: String(size) });
  if (status) params.set('status', status);
  return apiRequest<import('./types').ListFaultKnowledgeData>(`/knowledge/fault-cards?${params}`);
}

export async function approveFaultKnowledge(cardId: string) {
  return apiRequest<{ card_id: string; doc_id: string; task_id: string; status: string }>(
    `/knowledge/fault-cards/${cardId}/approve`,
    { method: 'POST' },
  );
}

export async function rejectFaultKnowledge(cardId: string) {
  return apiRequest<{ card_id: string; status: string }>(
    `/knowledge/fault-cards/${cardId}/reject`,
    { method: 'POST' },
  );
}

export async function feedbackFaultKnowledge(cardId: string, rating: 'useful' | 'bad', comment = '') {
  return apiRequest<{ card_id: string; rating: string; status: string }>(
    `/knowledge/fault-cards/${cardId}/feedback`,
    { method: 'POST', body: JSON.stringify({ rating, comment }) },
  );
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

export async function listVectorGCTasks(
  page = 1,
  size = 30,
  status?: import('./types').VectorGCTaskStatus,
) {
  const params = new URLSearchParams({ page: String(page), size: String(size) });
  if (status) params.set('status', status);
  return apiRequest<import('./types').ListVectorGCTasksData>(`/admin/vector-gc/tasks?${params}`);
}

export async function requestVectorGCRedrive(docId: string, targetKey: string, reason: string) {
  return apiRequest<{ approval_id: string; status: string }>(
    `/admin/vector-gc/documents/${encodeURIComponent(docId)}/tasks/${encodeURIComponent(targetKey)}/redrive`,
    { method: 'POST', body: JSON.stringify({ reason }) },
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
