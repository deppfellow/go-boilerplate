package middleware

import (
	"github.com/deppfellow/go-boilerplate/internal/server"
)

// RateLimitMiddleware is a helper around rate-limiting behavior.
//
// In this snippet, it does NOT enforce a limit.
// It only records a "RateLimitHit" event to New Relic when a limit is hit.
//
// Actual limiting logic (deny requests, token bucket, etc.) would be elsewhere.
type RateLimitMiddleware struct {
	// server holds access to shared dependencies like LoggerService (New Relic).
	server *server.Server
}

// NewRateLimitMiddleware constructs RateLimitMiddleware with access to app Server.
func NewRateLimitMiddleware(s *server.Server) *RateLimitMiddleware {
	return &RateLimitMiddleware{
		server: s,
	}
}

// RecordLateLimitHit records a custom event in New Relic when rate limiting occurs.
//
// Input:
//   - endpoint: usually the route/path name that was rate-limited
//
// Behavior:
//   - If New Relic is enabled, call RecordCustomEvent.
//   - If New Relic is disabled, do nothing (no-op).
//
// Output:
//   - No return value. This is best-effort telemetry.
func (r *RateLimitMiddleware) RecordLateLimitHit(endpoint string) {
	// Check that LoggerService exists and has a real New Relic application instance.
	if r.server.LoggerService != nil && r.server.LoggerService.GetApplication() != nil {
		// RecordCustomEvent creates a custom event in New Relic.
		// "RateLimitHit" is the event type name.
		// Attributes can be queried/filtered in New Relic.
		r.server.LoggerService.GetApplication().RecordCustomEvent(
			"RateLimitHit",
			map[string]interface{}{
				"endpoint": endpoint,
			},
		)
	}
}
