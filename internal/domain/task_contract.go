package domain

import (
	"strings"
)

// TaskContract is the immutable execution boundary for one Agent task.  It is
// intentionally data-only so it can be persisted with the task and replayed
// by a worker without trusting a fresh model decision.
type TaskContract struct {
	TaskID             string   `json:"task_id"`
	TenantID           string   `json:"tenant_id"`
	UserID             string   `json:"user_id"`
	Goal               string   `json:"goal"`
	AllowedTools       []string `json:"allowed_tools,omitempty"`
	AllowedResources   []string `json:"allowed_resources,omitempty"`
	ForbiddenActions   []string `json:"forbidden_actions,omitempty"`
	CompletionCriteria []string `json:"completion_criteria"`
	RiskBudget         int      `json:"risk_budget"`
	ConfigVersion      string   `json:"config_version,omitempty"`
	TraceID            string   `json:"trace_id"`
}

// TaskCompletion is the runtime's explicit completion proposal.  Today it is
// constructed from the plan-execute-replan terminal response; keeping it as a
// first-class value makes exposing a native TaskComplete tool backward
// compatible in the next iteration.
type TaskCompletion struct {
	Status      string          `json:"status"`
	Summary     string          `json:"summary"`
	EvidenceIDs []string        `json:"evidence_ids,omitempty"`
	Checklist   []ChecklistItem `json:"checklist"`
}

type ChecklistItem struct {
	Item      string `json:"item"`
	Completed bool   `json:"completed"`
}

// CompletionDecision is stable, client-safe evidence of why a task did or
// did not enter its successful terminal state.
type CompletionDecision struct {
	Accepted bool     `json:"accepted"`
	Reason   string   `json:"reason,omitempty"`
	Missing  []string `json:"missing,omitempty"`
}

const (
	CompletionAccepted       = "accepted"
	CompletionToolMissing    = "tool_missing"
	CompletionTaskIncomplete = "task_incomplete"
	CompletionBudgetExceeded = "budget_exceeded"
	CompletionScopeViolation = "scope_violation"
)

// ValidateTaskCompletion makes success depend on platform-recorded evidence,
// not on the model's prose. Evidence is accepted only when a successful tool
// call belongs to the immutable contract allowlist.
func ValidateTaskCompletion(contract TaskContract, completion TaskCompletion, evidence []Evidence, iterations int) CompletionDecision {
	if contract.RiskBudget > 0 && iterations > contract.RiskBudget {
		return CompletionDecision{Reason: CompletionBudgetExceeded}
	}
	if strings.TrimSpace(completion.Status) != "completed" {
		return CompletionDecision{Reason: CompletionTaskIncomplete, Missing: []string{"explicit task_complete"}}
	}
	if strings.TrimSpace(completion.Summary) == "" {
		return CompletionDecision{Reason: CompletionTaskIncomplete, Missing: []string{"summary"}}
	}
	for _, item := range completion.Checklist {
		if strings.TrimSpace(item.Item) != "" && !item.Completed {
			return CompletionDecision{Reason: CompletionTaskIncomplete, Missing: []string{item.Item}}
		}
	}
	allowed := make(map[string]struct{}, len(contract.AllowedTools))
	for _, tool := range contract.AllowedTools {
		if tool = strings.TrimSpace(tool); tool != "" {
			allowed[tool] = struct{}{}
		}
	}
	hasEvidence := false
	for _, item := range evidence {
		if item.Status == "error" || item.Status == "timeout" {
			return CompletionDecision{Reason: CompletionTaskIncomplete, Missing: []string{"handled failed tool step"}}
		}
		if item.Status != "success" {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[item.ToolName]; !ok {
				return CompletionDecision{Reason: CompletionScopeViolation, Missing: []string{item.ToolName}}
			}
		}
		hasEvidence = true
	}
	if !hasEvidence {
		return CompletionDecision{Reason: CompletionToolMissing, Missing: []string{"successful tool evidence"}}
	}
	return CompletionDecision{Accepted: true, Reason: CompletionAccepted}
}
