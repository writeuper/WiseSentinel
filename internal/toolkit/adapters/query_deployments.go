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

	"wisesentinel-platform/internal/pkg/configx"
)

// DeploymentsInput is the input for query_deployments.
type DeploymentsInput struct {
	Service string `json:"service,omitempty"`
	Env     string `json:"env,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Hours   int    `json:"hours,omitempty"` // lookback window in hours, default 24
}

// DeploymentsOutput is the formatted output for query_deployments.
type DeploymentsOutput struct {
	Deployments []DeploymentRecord `json:"deployments"`
	Count       int                `json:"count"`
	Source      string             `json:"source"`
}

// DeploymentRecord is a single deployment / release entry.
type DeploymentRecord struct {
	ReleaseID string            `json:"release_id,omitempty"`
	Service   string            `json:"service"`
	Env       string            `json:"env,omitempty"`
	Version   string            `json:"version,omitempty"`
	Operator  string            `json:"operator,omitempty"`
	Status    string            `json:"status,omitempty"`
	Strategy  string            `json:"strategy,omitempty"` // rolling / canary / blue-green
	StartTime string            `json:"start_time,omitempty"`
	EndTime   string            `json:"end_time,omitempty"`
	CommitID  string            `json:"commit_id,omitempty"`
	CommitMsg string            `json:"commit_msg,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// NewQueryDeployments returns an adapter that queries the release / deployment system.
//
// The adapter talks to a configurable HTTP endpoint (deployment.base_url) that is
// expected to return a list of deployments in a WiseSentinel-compatible shape.
// When the endpoint is not configured the adapter returns a clear
// "not_configured" status so the Ops Agent can still reason about the gap.
func NewQueryDeployments() func(ctx context.Context, input json.RawMessage) (string, error) {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var req DeploymentsInput
		if err := json.Unmarshal(input, &req); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}
		if req.Service == "" {
			return "", fmt.Errorf("service is required")
		}

		baseURL := configx.String(ctx, "deployment.base_url", "DEPLOYMENT_BASE_URL")
		if strings.EqualFold(baseURL, "local_mock") {
			return localMockDeployments(req), nil
		}
		if baseURL == "" {
			out := DeploymentsOutput{Source: "not_configured"}
			raw, _ := json.Marshal(out)
			return string(raw), nil
		}

		hours := req.Hours
		if hours <= 0 {
			hours = 24
		}
		limit := req.Limit
		if limit <= 0 {
			limit = 20
		}

		q := url.Values{"service": {req.Service}, "hours": {strconv.Itoa(hours)}, "limit": {strconv.Itoa(limit)}}
		if req.Env != "" {
			q.Set("env", req.Env)
		}

		u := strings.TrimRight(baseURL, "/") + "/api/v1/deployments?" + q.Encode()
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", fmt.Errorf("create deployment request: %w", err)
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(httpReq)
		if err != nil {
			return "", fmt.Errorf("deployment request failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return "", upstreamHTTPError("deployment", resp)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("read deployment response: %w", err)
		}
		// Accept either {"deployments":[...]} or a bare array.
		out := DeploymentsOutput{Source: "deployment_api"}
		if err := json.Unmarshal(body, &out.Deployments); err == nil && out.Deployments != nil {
			// bare array path
		} else {
			var wrapper struct {
				Deployments []DeploymentRecord `json:"deployments"`
				Items       []DeploymentRecord `json:"items"`
			}
			if err := json.Unmarshal(body, &wrapper); err != nil {
				return "", fmt.Errorf("parse deployment response: %w", err)
			}
			if len(wrapper.Deployments) > 0 {
				out.Deployments = wrapper.Deployments
			} else {
				out.Deployments = wrapper.Items
			}
		}
		out.Count = len(out.Deployments)

		raw, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("marshal output: %w", err)
		}
		return string(raw), nil
	}
}
