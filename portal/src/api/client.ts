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

export async function login(username: string, password: string) {
  return apiRequest<import('./types').AuthTokenData>('/auth/token', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  });
}

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
