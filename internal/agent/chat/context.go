package chat

import (
	"context"
	"strings"
	"unicode/utf8"

	"wisesentinel-platform/internal/domain"

	"github.com/gogf/gf/v2/frame/g"
)

const (
	defaultHistoryTokenBudget = 4000
	defaultMessageCharLimit   = 8000
)

type contextLimits struct {
	historyTokens    int
	messageCharLimit int
}

func compactHistory(history []*domain.Message, query string, limits contextLimits) []*domain.Message {
	if limits.historyTokens <= 0 {
		limits.historyTokens = defaultHistoryTokenBudget
	}
	if limits.messageCharLimit <= 0 {
		limits.messageCharLimit = defaultMessageCharLimit
	}

	selected := make([]*domain.Message, 0, len(history))
	used := estimateTokens(query)
	for i := len(history) - 1; i >= 0; i-- {
		message := history[i]
		if message == nil {
			continue
		}
		content := truncateMessage(message.Content, limits.messageCharLimit)
		candidate := &domain.Message{Role: message.Role, Content: content, Timestamp: message.Timestamp}
		cost := estimateTokens(content)
		if used+cost > limits.historyTokens {
			continue
		}
		selected = append(selected, candidate)
		used += cost
	}

	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}
	return selected
}

func estimateTokens(value string) int {
	if value == "" {
		return 0
	}
	return utf8.RuneCountInString(value)
}

func truncateMessage(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…[truncated]"
}

func readContextInt(ctx context.Context, key string, fallback int) int {
	value := g.Cfg().MustGet(ctx, key, fallback).Int()
	if value <= 0 {
		return fallback
	}
	return value
}
