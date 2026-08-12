package handler

import (
	"net/http"

	"wisesentinel-platform/internal/bootstrap"
	"wisesentinel-platform/internal/pkg/response"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// RegisterHealth mounts liveness and readiness probes.
func RegisterHealth(s *ghttp.Server, app *bootstrap.App) {
	s.BindHandler("/health/live", func(r *ghttp.Request) {
		r.Response.WriteJson(response.OK(g.Map{"status": "up"}))
	})

	s.BindHandler("/health/ready", func(r *ghttp.Request) {
		components := app.Ready(r.Context())
		ready := true
		for name, status := range components {
			if status != "up" && status != "skipped" && !(status == "degraded" && name == "rag") {
				ready = false
				break
			}
		}

		code := http.StatusOK
		if !ready {
			code = http.StatusServiceUnavailable
		}
		r.Response.Status = code
		r.Response.WriteJson(response.Body{
			Code: 0,
			Message: func() string {
				if ready {
					return "ready"
				}
				return "not ready"
			}(),
			Data: g.Map{
				"ready":      ready,
				"components": components,
			},
		})
	})
}
