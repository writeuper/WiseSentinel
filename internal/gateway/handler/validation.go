package handler

import (
	"strings"
	"unicode/utf8"

	"wisesentinel-platform/internal/pkg/apperr"
)

// Request limits bound values that are copied into model prompts, session
// memory, durable tasks, audit records, or reviewer comments. Transport-level
// body limits alone do not protect these individual high-cost fields.
const (
	maxSessionTitleRunes = 256
	maxChatQuestionRunes = 12000
	maxOpsQueryRunes     = 8000
	maxCommentRunes      = 2000
)

func validateBoundedText(value string, maxRunes int) error {
	if maxRunes <= 0 || strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > maxRunes {
		return apperr.ErrBadRequest
	}
	return nil
}
