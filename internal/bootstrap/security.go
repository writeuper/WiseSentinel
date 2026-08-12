package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func validateRuntimeSecurity(_ context.Context) error {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		for key, value := range map[string]string{
			"JWT_SECRET":   os.Getenv("JWT_SECRET"),
			"DEV_API_KEY":  os.Getenv("DEV_API_KEY"),
			"DEV_PASSWORD": os.Getenv("DEV_PASSWORD"),
		} {
			if value == "" || value == "change-me-in-production" || value == "ws-dev-key" || value == "dev123" {
				return fmt.Errorf("insecure production credential: %s", key)
			}
		}
	}
	return nil
}
