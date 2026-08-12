package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueryPrometheusAlertsHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/alerts" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer alerts-token" {
			t.Errorf("unexpected authorization header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"alerts":[{"labels":{"alertname":"HighError","severity":"critical"},"annotations":{"description":"error rate high"},"state":"firing","startsAt":"2026-08-04T10:00:00Z"}]}}`))
	}))
	defer server.Close()
	t.Setenv("PROMETHEUS_URL", server.URL)
	t.Setenv("PROMETHEUS_BEARER_TOKEN", "alerts-token")

	output, err := QueryPrometheusAlerts(context.Background(), nil)
	if err != nil {
		t.Fatalf("QueryPrometheusAlerts failed: %v", err)
	}
	var got PrometheusAlertsOutput
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if got.Source != "prometheus" || got.IsMock {
		t.Fatalf("unexpected metadata: %+v", got)
	}
	if got.Count != 1 || got.Alerts[0].Name != "HighError" {
		t.Fatalf("unexpected alerts: %+v", got)
	}
}

func TestQueryPrometheusAlertsErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "http error", status: http.StatusBadGateway, body: "upstream failed", want: "prometheus HTTP 502"},
		{name: "prometheus error", status: http.StatusOK, body: `{"status":"error","error":"bad query"}`, want: "prometheus query failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			t.Setenv("PROMETHEUS_URL", server.URL)
			t.Setenv("PROMETHEUS_BEARER_TOKEN", "")
			_, err := QueryPrometheusAlerts(context.Background(), nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestQueryPrometheusAlertsMockMetadata(t *testing.T) {
	t.Setenv("PROMETHEUS_URL", "local_mock")
	t.Setenv("PROMETHEUS_BEARER_TOKEN", "ignored")
	output, err := QueryPrometheusAlerts(context.Background(), nil)
	if err != nil {
		t.Fatalf("QueryPrometheusAlerts failed: %v", err)
	}
	var got PrometheusAlertsOutput
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if got.Source != "local_mock" || !got.IsMock {
		t.Fatalf("unexpected mock metadata: %+v", got)
	}
}

func TestQueryMetricRangeHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "up" {
			t.Errorf("unexpected query: %q", r.URL.Query().Get("query"))
		}
		if got := r.Header.Get("Authorization"); got != "Bearer metrics-token" {
			t.Errorf("unexpected authorization header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"job":"api"},"values":[[1722765600, "1"], [1722765660, "2"]]}]}}`))
	}))
	defer server.Close()
	t.Setenv("PROMETHEUS_URL", server.URL)
	t.Setenv("PROMETHEUS_BEARER_TOKEN", "metrics-token")

	output, err := QueryMetricRange(context.Background(), json.RawMessage(`{"query":"up","start":"1722765600","end":"1722765660","step":"60s"}`))
	if err != nil {
		t.Fatalf("QueryMetricRange failed: %v", err)
	}
	var got MetricRangeOutput
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if got.Source != "prometheus" || got.IsMock || got.Count != 1 || len(got.Series[0].Points) != 2 {
		t.Fatalf("unexpected output: %+v", got)
	}
}

func TestQueryMetricRangeErrorsAndMock(t *testing.T) {
	t.Run("http error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("temporarily unavailable"))
		}))
		defer server.Close()
		t.Setenv("PROMETHEUS_URL", server.URL)
		_, err := QueryMetricRange(context.Background(), json.RawMessage(`{"query":"up"}`))
		if err == nil || !strings.Contains(err.Error(), "prometheus HTTP 503") {
			t.Fatalf("expected HTTP error, got %v", err)
		}
	})

	t.Run("prometheus error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status":"error","error":"bad expression"}`))
		}))
		defer server.Close()
		t.Setenv("PROMETHEUS_URL", server.URL)
		_, err := QueryMetricRange(context.Background(), json.RawMessage(`{"query":"up"}`))
		if err == nil || !strings.Contains(err.Error(), "prometheus query failed") || strings.Contains(err.Error(), "bad expression") {
			t.Fatalf("expected Prometheus error, got %v", err)
		}
	})

	t.Run("mock metadata", func(t *testing.T) {
		t.Setenv("PROMETHEUS_URL", "local_mock")
		output, err := QueryMetricRange(context.Background(), json.RawMessage(`{"query":"up"}`))
		if err != nil {
			t.Fatalf("QueryMetricRange failed: %v", err)
		}
		var got MetricRangeOutput
		if err := json.Unmarshal([]byte(output), &got); err != nil {
			t.Fatalf("decode output: %v", err)
		}
		if got.Source != "local_mock" || !got.IsMock {
			t.Fatalf("unexpected mock metadata: %+v", got)
		}
	})
}
