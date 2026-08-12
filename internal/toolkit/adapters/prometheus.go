package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PrometheusAlertsInput is the input for query_prometheus_alerts.
type PrometheusAlertsInput struct {
	Query string `json:"query,omitempty"`
}

// PrometheusAlertsOutput is the formatted output.
type PrometheusAlertsOutput struct {
	Alerts []PrometheusAlert `json:"alerts"`
	Count  int               `json:"count"`
	Source string            `json:"source"`
	IsMock bool              `json:"is_mock"`
}

// PrometheusAlert is a simplified alert from Prometheus.
type PrometheusAlert struct {
	Name        string            `json:"name"`
	State       string            `json:"state"`
	Severity    string            `json:"severity"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	StartsAt    string            `json:"starts_at"`
}

// QueryPrometheusAlerts fetches alerts from Prometheus API.
func QueryPrometheusAlerts(ctx context.Context, input json.RawMessage) (string, error) {
	baseURL, bearerToken := prometheusConfig(ctx)
	if strings.EqualFold(baseURL, "local_mock") {
		return localMockAlerts(), nil
	}
	if baseURL == "" {
		baseURL = "http://127.0.0.1:9090"
	}

	url := strings.TrimRight(baseURL, "/") + "/api/v1/alerts"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	setPrometheusAuth(req, bearerToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
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
	// Parse standard Prometheus API response
	var promResp struct {
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
		Data   struct {
			Alerts []struct {
				Labels      map[string]string `json:"labels"`
				Annotations map[string]string `json:"annotations"`
				State       string            `json:"state"`
				StartsAt    string            `json:"startsAt"`
			} `json:"alerts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &promResp); err != nil {
		return "", fmt.Errorf("parse prometheus response: %w", err)
	}
	if promResp.Status != "success" {
		return "", fmt.Errorf("prometheus query failed")
	}

	// Deduplicate by alertname, keep first
	seen := make(map[string]bool)
	output := PrometheusAlertsOutput{Source: "prometheus"}
	for _, a := range promResp.Data.Alerts {
		name := a.Labels["alertname"]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		severity := a.Labels["severity"]
		if severity == "" {
			severity = "unknown"
		}
		output.Alerts = append(output.Alerts, PrometheusAlert{
			Name:        name,
			State:       a.State,
			Severity:    severity,
			Description: a.Annotations["description"],
			Labels:      a.Labels,
			StartsAt:    a.StartsAt,
		})
	}
	output.Count = len(output.Alerts)

	result, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("marshal output: %w", err)
	}
	return string(result), nil
}
