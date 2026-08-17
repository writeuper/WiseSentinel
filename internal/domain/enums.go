package domain

// AgentType identifies which specialist agent handles a request.
type AgentType string

const (
	AgentTypeChat      AgentType = "chat"
	AgentTypeOps       AgentType = "ops"
	AgentTypeKnowledge AgentType = "knowledge"
)

// ToolRiskLevel classifies tool invocation risk.
type ToolRiskLevel string

const (
	ToolRiskL0Readonly      ToolRiskLevel = "L0_READONLY"
	ToolRiskL1SensitiveRead ToolRiskLevel = "L1_SENSITIVE_READ"
	ToolRiskL2Write         ToolRiskLevel = "L2_WRITE"
)

// Role defines RBAC roles.
type Role string

const (
	RoleViewer        Role = "viewer"
	RoleOperator      Role = "operator"
	RoleSREAdmin      Role = "sre_admin"
	RolePlatformAdmin Role = "platform_admin"
)

// IndexTaskStatus tracks knowledge index job state.
type IndexTaskStatus string

const (
	IndexTaskPending   IndexTaskStatus = "pending"
	IndexTaskRunning   IndexTaskStatus = "running"
	IndexTaskRetryWait IndexTaskStatus = "retry_wait"
	IndexTaskSuccess   IndexTaskStatus = "success"
	IndexTaskFailed    IndexTaskStatus = "failed"
)

// OpsTaskStatus tracks ops analysis job state.
type OpsTaskStatus string

const (
	OpsTaskPending          OpsTaskStatus = "pending"
	OpsTaskRunning          OpsTaskStatus = "running"
	OpsTaskRetrying         OpsTaskStatus = "retrying"
	OpsTaskTimeout          OpsTaskStatus = "timeout"
	OpsTaskSuccess          OpsTaskStatus = "success"
	OpsTaskFailed           OpsTaskStatus = "failed"
	OpsTaskAwaitingApproval OpsTaskStatus = "awaiting_approval"
	OpsTaskIncomplete       OpsTaskStatus = "incomplete"
	OpsTaskCancelled        OpsTaskStatus = "cancelled"
)

// ApprovalStatus tracks human-in-the-loop approval state.
type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
	ApprovalExpired  ApprovalStatus = "expired"
)

const DefaultTenantID = "default"
