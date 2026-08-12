package metrics

import "testing"

func TestNormalizePathEliminatesDynamicMetricLabels(t *testing.T) {
	cases := map[string]string{
		"/api/v1/traces/4b1bc508f3d3479a882eaf61e7d782cd":             "/api/v1/traces/{trace_id}",
		"/api/v1/sessions/sess-a/messages":                            "/api/v1/sessions/{id}/messages",
		"/api/v1/knowledge/documents/doc-a/reindex":                   "/api/v1/knowledge/documents/{id}/reindex",
		"/api/v1/knowledge/fault-cards/card-a/approve":                "/api/v1/knowledge/fault-cards/{id}/approve",
		"/api/v1/admin/vector-gc/documents/doc-a/tasks/key-a/redrive": "/api/v1/admin/vector-gc/documents/{doc_id}/tasks/{target_key}/redrive",
		"/scanner/random-tenant-or-trace-id-123":                      "/unknown",
		"/api/v1/unknown/random-123":                                  "/unknown",
	}
	for path, want := range cases {
		if got := normalizePath(path); got != want {
			t.Errorf("normalizePath(%q) = %q, want %q", path, got, want)
		}
	}
}
