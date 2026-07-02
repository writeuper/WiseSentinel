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

export interface CreateSessionData {
  session_id: string;
  title: string;
  created_at: string;
}

export interface MessageItem {
  role: string;
  content: string;
  timestamp: string;
}

export interface ChatOptions {
  enable_rag: boolean;
  enable_tools: boolean;
}

export interface ChatRequest {
  session_id: string;
  question: string;
  options?: ChatOptions;
}

export interface ChatResponse {
  session_id: string;
  answer: string;
  citations?: CitationItem[];
  tool_calls?: ToolCallSummary[];
  trace_id: string;
}

export interface CitationItem {
  doc_id: string;
  chunk_id: string;
  source: string;
  snippet: string;
}

export interface ToolCallSummary {
  tool: string;
  status: string;
  latency_ms?: number;
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

export interface OpsAnalyzeData {
  task_id: string;
  status: string;
  result: string;
  detail: string[];
  trace_id: string;
}

export interface OpsTaskData {
  task_id: string;
  status: string;
  result: string;
  detail: string[];
}

export interface GetSessionMessagesData {
  session_id: string;
  messages: MessageItem[];
}