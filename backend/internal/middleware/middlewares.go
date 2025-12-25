package middleware

import (
	"github.com/deppfellow/go-boilerplate/internal/server"
	"github.com/newrelic/go-agent/v3/newrelic"
)

// Middlewares is a lightweight container that groups all middleware components
// used by the HTTP server.
//
// Why this exists:
//   - Avoid scattering middleware construction throughout routing/setup code.
//   - Provide a single place where shared dependencies (like *server.Server and
//     New Relic application instance) are wired into middleware.
//
// This is dependency injection in its simplest form: build once, reuse everywhere.
type Middlewares struct {
	// Global holds common middleware used across the whole API:
	// CORS, request logging, recovery, secure headers, and the global error handler.
	Global *GlobalMiddlewares

	// Auth provides authentication middleware (Clerk-based) and attaches user context.
	Auth *AuthMiddleware

	// ContextEnhancer enriches each request with a request-scoped logger
	// (request_id, method, path, ip, optional user & trace metadata).
	ContextEnhancer *ContextEnhancer

	// Tracing provides New Relic middleware and helpers to attach custom attributes
	// and notice errors on transactions.
	Tracing *TracingMiddleware

	// RateLimit is telemetry/utility around rate limit events (records New Relic custom events).
	// Note: the enforcement logic, if any, typically lives elsewhere.
	RateLimit *RateLimitMiddleware
}

// NewMiddlewares constructs all middleware components using the application container.
//
// It also extracts the New Relic application instance (if configured) from the server's
// LoggerService and injects it into TracingMiddleware.
//
// Behavior when New Relic is not configured:
// - nrApp will be nil.
// - tracing middleware should degrade into a no-op (no transactions, no attributes).
func NewMiddlewares(s *server.Server) *Middlewares {
	// Get New Relic application instance from server, if available.
	//
	// LoggerService is responsible for initializing New Relic.
	// If New Relic is disabled or misconfigured, GetApplication() returns nil.
	var nrApp *newrelic.Application
	if s.LoggerService != nil {
		nrApp = s.LoggerService.GetApplication()
	}

	// Construct all middleware "services" once and reuse them during router setup.
	return &Middlewares{
		Global:          NewGlobalMiddlewares(s),
		Auth:            NewAuthMiddleware(s),
		ContextEnhancer: NewContextEnhancer(s),
		Tracing:         NewTracingMiddleware(s, nrApp),
		RateLimit:       NewRateLimitMiddleware(s),
	}
}
