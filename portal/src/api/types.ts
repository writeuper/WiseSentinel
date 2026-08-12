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

export interface ReindexDocumentData {
  doc_id: string;
  task_id: string;
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
	 target?: ApprovalTarget;
}

export interface ApprovalTarget {
	 kind: string;
	 doc_id?: string;
	 target_key?: string;
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
  evidence?: OpsEvidence[];
  conclusion?: OpsFaultConclusion;
  timing?: OpsTiming;
}

export interface OpsTiming {
  queue_duration_ms: number;
  run_duration_ms: number;
  e2e_duration_ms: number;
  created_at?: string;
  started_at?: string;
  finished_at?: string;
}

export interface OpsEvidence {
  tool_name: string;
  source?: string;
  status: string;
  latency_ms?: number;
  timestamp?: string;
  suppressed: boolean;
  input_bytes?: number;
  output_bytes?: number;
}

export interface OpsFaultConclusion {
  symptom: string;
  impact: string;
  root_cause: string;
  workaround: string;
  remediation: string;
  confidence: string;
  source: string;
}

export interface OpsTaskData {
  task_id: string;
  status: string;
  result: string;
  detail: string[];
  evidence?: OpsEvidence[];
  conclusion?: OpsFaultConclusion;
  timing?: OpsTiming;
}

export interface AgentTraceData {
  trace: AgentTrace;
  steps: AgentTraceStep[];
}

export interface AgentTrace {
  trace_id: string;
  tenant_id: string;
  user_id: string;
  agent_type: string;
  session_id?: string;
  task_id?: string;
  query: string;
  status: string;
  latency_ms: number;
  error_msg?: string;
  started_at?: string;
  finished_at?: string;
}

export interface AgentTraceStep {
  id: number;
  step_type: string;
  step_name: string;
  input_summary?: string;
  output_summary?: string;
  status: string;
  latency_ms: number;
  error_msg?: string;
  created_at?: string;
}

// M5 user identity (parsed from JWT).
export type Role = 'viewer' | 'operator' | 'sre_admin' | 'platform_admin';

export interface CurrentUser {
  username: string;
  tenant_id: string;
  roles: Role[];
}

export interface ListOpsTasksData {
  items: OpsTaskSummary[];
  total: number;
}

export interface FaultKnowledgeItem {
  card_id: string;
  task_id: string;
  trace_id: string;
  title: string;
  symptom: string;
  impact: string;
  root_cause: string;
  workaround: string;
  remediation: string;
  service: string;
  version: string;
  status: string;
  weight: number;
  doc_id: string;
  hit_count: number;
  useful_count: number;
  bad_count: number;
  created_by: string;
  reviewed_by: string;
  created_at: string;
  updated_at: string;
  reviewed_at?: string;
}

export interface ListFaultKnowledgeData {
  items: FaultKnowledgeItem[];
  total: number;
}

export interface OpsTaskSummary {
  task_id: string;
  status: string;
  trigger_type: string;
  created_at: string;
}

export interface GetSessionMessagesData {
  session_id: string;
  messages: MessageItem[];
}

export type VectorGCTaskStatus = 'pending' | 'running' | 'retry_wait' | 'succeeded' | 'skipped' | 'dead';

export interface VectorGCTaskItem {
	  doc_id: string;
	  target_key: string;
	  target_kind: string;
	  target_generation: number;
	  status: VectorGCTaskStatus;
	  attempt_count: number;
	  max_attempts: number;
	  last_error?: string;
	  next_attempt_at?: string;
}

export interface ListVectorGCTasksData {
	  items: VectorGCTaskItem[];
	  total: number;
}
