package toolkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"
)

// ToolCallFingerprint is stable across JSON object key order. It intentionally
// includes tenant and task to prevent cross-tenant/task deduplication.
func ToolCallFingerprint(tenantID, taskID, toolName string, raw json.RawMessage, resourceScope string) string {
	canonical := CanonicalJSON(raw)
	s := strings.Join([]string{tenantID, taskID, toolName, canonical, resourceScope}, "\x1f")
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func CanonicalJSON(raw json.RawMessage) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "!invalid:" + string(raw)
	}
	return canonicalValue(value)
}
func canonicalValue(value any) string {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, strconv.Quote(k)+":"+canonicalValue(v[k]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		parts := make([]string, len(v))
		for i := range v {
			parts[i] = canonicalValue(v[i])
		}
		return "[" + strings.Join(parts, ",") + "]"
	default:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}

func validateToolInput(schema map[string]any, raw json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil || input == nil {
		return apperr.ErrSchemaInvalid
	}
	if required, ok := schema["required"].([]any); ok {
		for _, item := range required {
			key, _ := item.(string)
			if key != "" && (input[key] == nil || strings.TrimSpace(stringify(input[key])) == "") {
				return apperr.ErrArgumentMissing
			}
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	for key, ruleRaw := range properties {
		rule, _ := ruleRaw.(map[string]any)
		value, present := input[key]
		if !present {
			continue
		}
		if typ, _ := rule["type"].(string); typ != "" && !matchesJSONType(value, typ) {
			return apperr.ErrSchemaInvalid
		}
		if enum, ok := rule["enum"].([]any); ok && !containsJSONValue(enum, value) {
			return apperr.ErrArgumentOutOfRange
		}
		if n, ok := asFloat(value); ok {
			if min, has := asFloat(rule["minimum"]); has && n < min {
				return apperr.ErrArgumentOutOfRange
			}
			if max, has := asFloat(rule["maximum"]); has && n > max {
				return apperr.ErrArgumentOutOfRange
			}
		}
	}
	return nil
}
func stringify(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "present"
}
func matchesJSONType(v any, typ string) bool {
	switch typ {
	case "string":
		_, ok := v.(string)
		return ok
	case "integer":
		n, ok := v.(float64)
		return ok && n == float64(int64(n))
	case "number":
		_, ok := v.(float64)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	}
	return true
}
func asFloat(v any) (float64, bool) { n, ok := v.(float64); return n, ok }
func containsJSONValue(values []any, target any) bool {
	targetCanonical := canonicalValue(target)
	for _, v := range values {
		if canonicalValue(v) == targetCanonical {
			return true
		}
	}
	return false
}

func reserveToolBudget(ctx context.Context, tool string) error {
	b := ctxkeys.ToolBudgetFrom(ctx)
	s := ctxkeys.ToolBudgetStateFrom(ctx)
	if b == nil || s == nil {
		return nil
	}
	if b.MaxToolCalls > 0 && s.Calls >= b.MaxToolCalls {
		return apperr.ErrToolBudgetExceeded
	}
	if b.MaxSameToolCalls > 0 && s.ByTool[tool] >= b.MaxSameToolCalls {
		return apperr.ErrToolBudgetExceeded
	}
	if b.MaxRetryCount > 0 && s.Retries >= b.MaxRetryCount {
		return apperr.ErrToolBudgetExceeded
	}
	if b.MaxTotalToolLatencyMS > 0 && s.LatencyMS >= b.MaxTotalToolLatencyMS {
		return apperr.ErrToolBudgetExceeded
	}
	if b.MaxResponseBytes > 0 && s.ResponseBytes >= b.MaxResponseBytes {
		return apperr.ErrToolBudgetExceeded
	}
	s.Calls++
	s.ByTool[tool]++
	return nil
}
func commitToolBudget(ctx context.Context, latency int64, bytes int) {
	b := ctxkeys.ToolBudgetFrom(ctx)
	s := ctxkeys.ToolBudgetStateFrom(ctx)
	if b == nil || s == nil {
		return
	}
	s.LatencyMS += latency
	s.ResponseBytes += int64(bytes)
}

func validateQueryTimeRange(ctx context.Context, tool string, raw json.RawMessage) error {
	if tool != "query_metric_range" {
		return nil
	}
	b := ctxkeys.ToolBudgetFrom(ctx)
	if b == nil || b.MaxQueryTimeRangeMS <= 0 {
		return nil
	}
	var in struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}
	if json.Unmarshal(raw, &in) != nil || in.Start == "" || in.End == "" {
		return nil
	}
	start, okStart := parseToolTime(in.Start)
	end, okEnd := parseToolTime(in.End)
	if okStart && okEnd && end.Sub(start).Milliseconds() > b.MaxQueryTimeRangeMS {
		return apperr.ErrArgumentOutOfRange
	}
	return nil
}
func parseToolTime(value string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, true
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(seconds, 0), true
	}
	return time.Time{}, false
}
