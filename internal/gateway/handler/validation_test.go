package handler

import (
	"context"
	"reflect"
	"strings"
	"testing"

	v1 "wisesentinel-platform/api/v1"
)

func TestValidateBoundedTextHonorsRuneRatherThanByteBoundary(t *testing.T) {
	if err := validateBoundedText(strings.Repeat("测", 3), 3); err != nil {
		t.Fatalf("three CJK runes must fit a three-rune limit: %v", err)
	}
	if err := validateBoundedText(strings.Repeat("测", 4), 3); err == nil {
		t.Fatal("four CJK runes must exceed a three-rune limit")
	}
}

func TestValidateBoundedTextRejectsWhitespaceOnlyInput(t *testing.T) {
	for _, value := range []string{"", "   ", "\t\n", "　"} {
		if err := validateBoundedText(value, 10); err == nil {
			t.Fatalf("whitespace-only value %q must be rejected", value)
		}
	}
}

func TestHandlersRejectOversizedHighCostFieldsBeforeDependencies(t *testing.T) {
	controller := NewV1(nil)
	ctx := context.Background()
	if _, err := controller.CreateSession(ctx, &v1.CreateSessionReq{Title: strings.Repeat("t", maxSessionTitleRunes+1)}); err == nil {
		t.Fatal("oversized session title must be rejected")
	}
	if _, err := controller.Chat(ctx, &v1.ChatReq{Question: strings.Repeat("q", maxChatQuestionRunes+1)}); err == nil {
		t.Fatal("oversized chat question must be rejected")
	}
	if _, err := controller.OpsAnalyze(ctx, &v1.OpsAnalyzeReq{Query: strings.Repeat("o", maxOpsQueryRunes+1)}); err == nil {
		t.Fatal("oversized Ops query must be rejected")
	}
	if _, err := controller.ApprovalDecision(ctx, &v1.ApprovalDecisionReq{Comment: strings.Repeat("c", maxCommentRunes+1)}); err == nil {
		t.Fatal("oversized approval comment must be rejected")
	}
}

func TestHandlersRejectNilAgentRequests(t *testing.T) {
	controller := NewV1(nil)
	ctx := context.Background()
	if _, err := controller.Chat(ctx, nil); err == nil {
		t.Fatal("Chat(nil) should return bad request")
	}
	if _, err := controller.ChatStream(ctx, nil); err == nil {
		t.Fatal("ChatStream(nil) should return bad request")
	}
	if _, err := controller.OpsAnalyze(ctx, nil); err == nil {
		t.Fatal("OpsAnalyze(nil) should return bad request")
	}
	if _, err := controller.AlertWebhook(ctx, nil); err == nil {
		t.Fatal("AlertWebhook(nil) should return bad request")
	}
	if _, err := controller.AlertmanagerWebhook(ctx, nil); err == nil {
		t.Fatal("AlertmanagerWebhook(nil) should return bad request")
	}
}

func TestHandlersRejectNilManagementRequests(t *testing.T) {
	controller := NewV1(nil)
	ctx := context.Background()
	checks := []struct {
		name string
		call func() error
	}{
		{"auth_token", func() error { _, err := controller.AuthToken(ctx, nil); return err }},
		{"create_session", func() error { _, err := controller.CreateSession(ctx, nil); return err }},
		{"list_sessions", func() error { _, err := controller.ListSessions(ctx, nil); return err }},
		{"get_session_messages", func() error { _, err := controller.GetSessionMessages(ctx, nil); return err }},
		{"delete_session", func() error { _, err := controller.DeleteSession(ctx, nil); return err }},
		{"get_ops_task", func() error { _, err := controller.GetOpsTask(ctx, nil); return err }},
		{"list_ops_tasks", func() error { _, err := controller.ListOpsTasks(ctx, nil); return err }},
		{"list_approvals", func() error { _, err := controller.ListApprovals(ctx, nil); return err }},
		{"approval_decision", func() error { _, err := controller.ApprovalDecision(ctx, nil); return err }},
		{"list_vector_gc", func() error { _, err := controller.ListVectorGCTasks(ctx, nil); return err }},
		{"request_vector_gc_redrive", func() error { _, err := controller.RequestVectorGCRedrive(ctx, nil); return err }},
		{"list_agent_configs", func() error { _, err := controller.ListAgentConfigs(ctx, nil); return err }},
		{"activate_agent_config", func() error { _, err := controller.ActivateAgentConfig(ctx, nil); return err }},
		{"get_trace", func() error { _, err := controller.GetTrace(ctx, nil); return err }},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("nil request must return an error")
			}
		})
	}
}

func TestRequestTextLimitsRejectOversizedPromptAndComment(t *testing.T) {
	for name, value := range map[string]struct {
		value string
		limit int
	}{
		"session_title": {strings.Repeat("t", maxSessionTitleRunes+1), maxSessionTitleRunes},
		"chat_question": {strings.Repeat("q", maxChatQuestionRunes+1), maxChatQuestionRunes},
		"ops_query":     {strings.Repeat("o", maxOpsQueryRunes+1), maxOpsQueryRunes},
		"comment":       {strings.Repeat("c", maxCommentRunes+1), maxCommentRunes},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateBoundedText(value.value, value.limit); err == nil {
				t.Fatalf("oversized %s must be rejected", name)
			}
		})
	}
}

func TestVectorGCRedriveReasonUsesSupportedValidationRules(t *testing.T) {
	// The request tag is parsed by GoFrame before the handler runs. Keep this
	// contract test close to the API type so an unsupported compound rule cannot
	// silently disable every redrive request again.
	field, ok := reflect.TypeOf(v1.RequestVectorGCRedriveReq{}).FieldByName("Reason")
	if !ok || field.Tag.Get("v") != "required|length:1,500" {
		t.Fatalf("redrive reason validation tag = %q, want required|length:1,500", field.Tag.Get("v"))
	}
	for _, value := range []string{"Milvus recovered", strings.Repeat("x", 500)} {
		if err := validateBoundedText(value, 500); err != nil {
			t.Fatalf("valid redrive reason rejected: %v", err)
		}
	}
}
