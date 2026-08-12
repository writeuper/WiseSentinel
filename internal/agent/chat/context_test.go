package chat

import (
	"strings"
	"testing"

	"wisesentinel-platform/internal/domain"
)

func TestCompactHistoryKeepsNewestMessagesWithinBudget(t *testing.T) {
	history := []*domain.Message{
		{Role: "user", Content: "old-user"},
		{Role: "assistant", Content: "old-assistant"},
		{Role: "user", Content: "new-user"},
		{Role: "assistant", Content: "new-assistant"},
	}

	got := compactHistory(history, "query", contextLimits{historyTokens: 30, messageCharLimit: 100})
	if len(got) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(got))
	}
	if got[0].Content != "new-user" || got[1].Content != "new-assistant" {
		t.Fatalf("history = %#v, want newest messages in order", got)
	}
}

func TestCompactHistoryTruncatesLargeMessage(t *testing.T) {
	got := compactHistory([]*domain.Message{{Role: "user", Content: "abcdef"}}, "q", contextLimits{historyTokens: 20, messageCharLimit: 3})
	if len(got) != 1 {
		t.Fatalf("len(history) = %d, want 1", len(got))
	}
	if got[0].Content != "abc…[truncated]" {
		t.Fatalf("content = %q, want truncated content", got[0].Content)
	}
}

func TestCompactHistorySkipsOversizedOldMessage(t *testing.T) {
	got := compactHistory([]*domain.Message{
		{Role: "user", Content: strings.Repeat("x", 100)},
		{Role: "assistant", Content: "recent"},
	}, "q", contextLimits{historyTokens: 10, messageCharLimit: 1000})
	if len(got) != 1 || got[0].Content != "recent" {
		t.Fatalf("history = %#v, want recent message only", got)
	}
}
