package handler

import (
	"context"
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
