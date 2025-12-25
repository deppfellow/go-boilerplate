// Package router initializes the HTTP router (using Echo).
//
// It registers global middleware and defines the API route groups,
// mapping specific paths to their corresponding handlers.
//
// Tutor notes (02:50:35–02:51:55):
//   - Create a single router constructor that wires middleware and handlers together.
//   - Attach the global error handler early so any error returned by handlers/middleware
//     becomes a consistent JSON response.
//   - Register global middleware in a single place to enforce consistent behavior
//     across the entire API (CORS, request_id, tracing, request logging, recovery, etc.).
package router

import (
	"net/http"

	"github.com/deppfellow/go-boilerplate/internal/handler"
	"github.com/deppfellow/go-boilerplate/internal/middleware"
	"github.com/deppfellow/go-boilerplate/internal/server"
	"github.com/deppfellow/go-boilerplate/internal/service"
	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

// Package router initializes the HTTP router (using Echo).
//
// It registers global middleware and defines the API route groups,
// mapping specific paths to their corresponding handlers.
//
// Tutor notes (02:50:35–02:51:55):
//   - Create a single router constructor that wires middleware and handlers together.
//   - Attach the global error handler early so any error returned by handlers/middleware
//     becomes a consistent JSON response.
//   - Register global middleware in a single place to enforce consistent behavior
//     across the entire API (CORS, request_id, tracing, request logging, recovery, etc.).
func NewRouter(s *server.Server, h *handler.Handlers, services *service.Services) *echo.Echo {
	// Construct middleware bundle (DI container).
	middlewares := middleware.NewMiddlewares(s)

	// Construct middleware bundle (DI container).
	router := echo.New()

	// Create the Echo router instance.
	router.HTTPErrorHandler = middlewares.Global.GlobalErrorHandler

	// Global middleware registration.
	//
	// Middleware order matters at runtime because later middleware can only use
	// values injected by earlier ones. This setup aims to ensure:
	// - request_id is available early
	// - tracing transaction exists before EnhanceTracing
	// - context enhancer can attach trace/user/request fields to logger
	// - request logger runs after context enrichment so logs include correlation fields
	router.Use(
		// Rate limiter middleware (Echo built-in).
		//
		// - Uses in-memory store with a limit of 20 requests/second (rate.Limit(20)).
		// - DenyHandler runs when the request is rejected.
		// - DenyHandler logs rate limit events and records a New Relic custom event.
		//
		// NOTE: In-memory limiter is per-instance. In multi-instance deployments,
		// each instance has its own limiter unless you use a distributed store (Redis).
		echoMiddleware.RateLimiterWithConfig(echoMiddleware.RateLimiterConfig{
			Store: echoMiddleware.NewRateLimiterMemoryStore(rate.Limit(20)),
			DenyHandler: func(c echo.Context, identifier string, err error) error {
				// Record rate limit hit telemetry (New Relic custom event) if enabled.
				if rateLimitMiddleware := middlewares.RateLimit; rateLimitMiddleware != nil {
					rateLimitMiddleware.RecordLateLimitHit(c.Path())
				}

				// Log rate limit rejection with useful correlation fields.
				s.Logger.Warn().
					Str("request_id", middleware.GetRequestID(c)).
					Str("identifier", identifier).     // identifier depends on limiter config (often IP)
					Str("path", c.Path()).             // route template path
					Str("method", c.Request().Method). // HTTP method
					Str("ip", c.RealIP()).             // client IP (respects proxy headers)
					Msg("rate limit exceeded")

				// Return a 429 error.
				// The global error handler will format the final JSON response.
				return echo.NewHTTPError(http.StatusTooManyRequests, "Rate limit exceeded")
			},
		}),

		// CORS policy configured via env/config.
		middlewares.Global.CORS(),

		// Secure headers middleware.
		middlewares.Global.Secure(),

		// Request ID middleware: reads X-Request-ID or generates UUID, stores it in context.
		middleware.RequestID(),

		// New Relic transaction middleware.
		// This must run before EnhanceTracing so a transaction exists in request context.
		middlewares.Tracing.NewRelicMiddleware(),

		// New Relic transaction middleware.
		// This must run before EnhanceTracing so a transaction exists in request context.
		middlewares.Tracing.EnhanceTracing(),

		// Builds a request-scoped logger and stores it in Echo context and Go context.
		// It uses request_id and optionally trace/user metadata if already available.
		middlewares.ContextEnhancer.EnhanceContext(),

		// Structured request logging (zerolog), using the enhanced logger from context.
		middlewares.Global.RequestLogger(),

		// Panic recovery middleware.
		middlewares.Global.Recover(),
	)

	// Register system/utility routes such as:
	// - /health
	// - /openapi /swagger
	registerSystemRoutes(router, h)

	// Register versioned routes
	router.Group("api/v1")

	return router
}
