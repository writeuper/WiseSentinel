export interface ApiResponse<T> {
  code: number;
  message: string;
  data?: T;
}

export interface AuthTokenData {
  access_token: string;
  expires_in: number;
  token_type: string;
}

export interface DocumentItem {
  doc_id: string;
  name: string;
  status: string;
  visibility: string;
  updated_at: string;
}

export interface UploadDocumentData {
  doc_id: string;
  task_id: string;
  file_name: string;
  file_size: number;
  status: string;
}

export interface IndexTaskData {
  task_id: string;
  doc_id: string;
  status: string;
  chunk_count: number;
  error_msg?: string;
}

export interface SessionItem {
  session_id: string;
  title: string;
  agent_type: string;
  updated_at: string;
}

export interface ApprovalItem {
  approval_id: string;
  task_id: string;
  approval_type: string;
  status: string;
  expired_at: string;
}

export interface AgentConfigItem {
  agent_type: string;
  version: string;
  is_active: boolean;
  created_at: string;
}
