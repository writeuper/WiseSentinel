package domain

import "testing"

func TestValidateTaskCompletion(t *testing.T) {
	contract := TaskContract{AllowedTools: []string{"query_logs"}, RiskBudget: 2}
	complete := TaskCompletion{Status: "completed", Summary: "done", Checklist: []ChecklistItem{{Item: "query logs", Completed: true}}}
	cases := []struct {
		name       string
		completion TaskCompletion
		evidence   []Evidence
		iterations int
		want       string
	}{
		{"accepts recorded evidence", complete, []Evidence{{ToolName: "query_logs", Status: "success"}}, 1, CompletionAccepted},
		{"rejects model-only conclusion", complete, nil, 1, CompletionToolMissing},
		{"rejects unchecked checklist", TaskCompletion{Summary: "done", Checklist: []ChecklistItem{{Item: "query logs", Completed: false}}}, []Evidence{{ToolName: "query_logs", Status: "success"}}, 1, CompletionTaskIncomplete},
		{"rejects budget exhaustion", complete, []Evidence{{ToolName: "query_logs", Status: "success"}}, 3, CompletionBudgetExceeded},
		{"rejects out of scope evidence", complete, []Evidence{{ToolName: "query_metric_range", Status: "success"}}, 1, CompletionScopeViolation},
		{"rejects unhandled failed step", complete, []Evidence{{ToolName: "query_logs", Status: "success"}, {ToolName: "query_metric_range", Status: "error"}}, 1, CompletionTaskIncomplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateTaskCompletion(contract, tc.completion, tc.evidence, tc.iterations).Reason; got != tc.want {
				t.Fatalf("reason = %q, want %q", got, tc.want)
			}
		})
	}
}
