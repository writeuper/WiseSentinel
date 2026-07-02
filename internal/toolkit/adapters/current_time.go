package adapters

import (
	"context"
	"encoding/json"
	"fmt"
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