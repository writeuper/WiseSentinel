package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CurrentTimeOutput is the output for get_current_time.
type CurrentTimeOutput struct {
	Time     string `json:"time"`
	Timezone string `json:"timezone"`
	RFC3339  string `json:"rfc3339"`
}

// GetCurrentTime returns the current server time.
func GetCurrentTime(_ context.Context, _ json.RawMessage) (string, error) {
	now := time.Now()
	output := CurrentTimeOutput{
		Time:     now.Format("2006-01-02 15:04:05"),
		Timezone: "Asia/Shanghai",
		RFC3339:  now.UTC().Format(time.RFC3339),
	}
	result, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("marshal output: %w", err)
	}
	return string(result), nil
}

// ConvertTime performs a bounded local timezone conversion. It is used as a
// deterministic fallback when the optional MCP time server is unavailable;
// the MCP adapter remains the preferred path when it is healthy.
func ConvertTime(_ context.Context, input json.RawMessage) (string, error) {
	var args struct {
		SourceTimezone string `json:"source_timezone"`
		Time           string `json:"time"`
		TargetTimezone string `json:"target_timezone"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if strings.TrimSpace(args.SourceTimezone) == "" || strings.TrimSpace(args.Time) == "" || strings.TrimSpace(args.TargetTimezone) == "" {
		return "", fmt.Errorf("source_timezone, time, and target_timezone are required")
	}
	source, err := time.LoadLocation(args.SourceTimezone)
	if err != nil {
		return "", fmt.Errorf("invalid source timezone")
	}
	target, err := time.LoadLocation(args.TargetTimezone)
	if err != nil {
		return "", fmt.Errorf("invalid target timezone")
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", args.Time, source)
	if err != nil {
		parsed, err = time.ParseInLocation(time.RFC3339, args.Time, source)
	}
	if err != nil {
		return "", fmt.Errorf("invalid time")
	}
	converted := parsed.In(target)
	result, err := json.Marshal(map[string]string{
		"source_timezone": args.SourceTimezone,
		"target_timezone": args.TargetTimezone,
		"source_time":     args.Time,
		"time":            converted.Format("2006-01-02 15:04:05"),
		"rfc3339":         converted.Format(time.RFC3339),
	})
	if err != nil {
		return "", fmt.Errorf("marshal output: %w", err)
	}
	return string(result), nil
}
