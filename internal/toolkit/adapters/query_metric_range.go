package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MetricRangeInput is the input for query_metric_range.
type MetricRangeInput struct {
	Query  string `json:"query"`           // PromQL expression, e.g. rate(http_requests_total{code="500"}[5m])
	Start  string `json:"start"`           // RFC3339 or unix timestamp; empty = now-30m
	End    string `json:"end"`             // RFC3339 or unix timestamp; empty = now
	Step   string `json:"step"`            // e.g. "60s"; empty = 60s
	Limits int    `json:"limit,omitempty"` // max points per series, 0 = 120
}

// MetricRangeOutput is the formatted output for query_metric_range.
type MetricRangeOutput struct {
	Series []MetricSeries `json:"series"`
	Count  int            `json:"count"`
	Status string         `json:"status"`
	Source string         `json:"source"`
	IsMock bool           `json:"is_mock"`
}

// MetricSeries is a single time series returned by Prometheus.
type MetricSeries struct {
	Metric map[string]string `json:"metric"`
	Points []MetricPoint     `json:"points"`
}

// MetricPoint is one (timestamp, value) sample.
type MetricPoint struct {
	Timestamp float64 `json:"timestamp"`
	Value     float64 `json:"value"`
}

// QueryMetricRange runs a Prometheus range query (/api/v1/query_range).
func QueryMetricRange(ctx context.Context, input json.RawMessage) (string, error) {
	var req MetricRangeInput
	if len(input) == 0 {
		return "", fmt.Errorf("input is required")
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if req.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	baseURL, bearerToken := prometheusConfig(ctx)
	if strings.EqualFold(baseURL, "local_mock") {
		return localMockMetrics(req), nil
	}
	if baseURL == "" {
		baseURL = "http://127.0.0.1:9090"
	}

	now := time.Now()
	end := now
	if req.End != "" {
		if t, err := parseTime(req.End); err == nil {
			end = t
		}
	}
	start := end.Add(-30 * time.Minute)
	if req.Start != "" {
		if t, err := parseTime(req.Start); err == nil {
			start = t
		}
	}
	step := 60 * time.Second
	if req.Step != "" {
		if d, err := time.ParseDuration(req.Step); err == nil && d > 0 {
			step = d
		}
	}

	limit := req.Limits
	if limit <= 0 {
		limit = 120
	}

	u := strings.TrimRight(baseURL, "/") + "/api/v1/query_range?" + url.Values{
		"query": {req.Query},
		"start": {strconv.FormatFloat(float64(start.Unix()), 'f', -1, 64)},
		"end":   {strconv.FormatFloat(float64(end.Unix()), 'f', -1, 64)},
		"step":  {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)},
	}.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	setPrometheusAuth(httpReq, bearerToken)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("prometheus request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", upstreamHTTPError("prometheus", resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	var promResp struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string   `json:"metric"`
				Values [][]json.RawMessage `json:"values"` // [ts, value]
			} `json:"result"`
		} `json:"data"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &promResp); err != nil {
		return "", fmt.Errorf("parse prometheus response: %w", err)
	}
	if promResp.Status != "success" {
		return "", fmt.Errorf("prometheus query failed")
	}

	out := MetricRangeOutput{Status: promResp.Status, Source: "prometheus"}
	for _, r := range promResp.Data.Result {
		pts := make([]MetricPoint, 0, len(r.Values))
		for _, v := range r.Values {
			if len(v) != 2 {
				continue
			}
			var timestamp, value float64
			if err := json.Unmarshal(v[0], &timestamp); err != nil {
				var timestampText string
				if json.Unmarshal(v[0], &timestampText) != nil {
					continue
				}
				timestamp, err = strconv.ParseFloat(timestampText, 64)
				if err != nil {
					continue
				}
			}
			if err := json.Unmarshal(v[1], &value); err != nil {
				var valueText string
				if json.Unmarshal(v[1], &valueText) != nil {
					continue
				}
				value, err = strconv.ParseFloat(valueText, 64)
				if err != nil {
					continue
				}
			}
			pts = append(pts, MetricPoint{Timestamp: timestamp, Value: value})
		}
		if len(pts) > limit {
			// downsample to the last N points to keep LLM context small
			pts = pts[len(pts)-limit:]
		}
		out.Series = append(out.Series, MetricSeries{Metric: r.Metric, Points: pts})
	}
	out.Count = len(out.Series)

	raw, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("marshal output: %w", err)
	}
	return string(raw), nil
}

// parseTime accepts RFC3339 or a unix timestamp string.
func parseTime(s string) (time.Time, error) {
	if t, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Unix(int64(t), 0), nil
	}
	return time.Parse(time.RFC3339, s)
}
