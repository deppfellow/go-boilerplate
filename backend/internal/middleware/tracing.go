package middleware

import (
	"github.com/labstack/echo/v4"
	"github.com/newrelic/go-agent/v3/integrations/nrecho-v4"
	"github.com/newrelic/go-agent/v3/integrations/nrpkgerrors"
	"github.com/newrelic/go-agent/v3/newrelic"

	"github.com/deppfellow/go-boilerplate/internal/server"
)

// TracingMiddleware owns New Relic related Echo middleware.
//
// It needs:
//   - server: for shared deps (logger/config) if needed later
//   - nrApp: the New Relic application instance (nil if New Relic disabled)
//
// This middleware has two layers:
//  1. NewRelicMiddleware()     -> installs New Relic transaction handling into Echo
//  2. EnhanceTracing()         -> adds custom attributes and notices errors
type TracingMiddleware struct {
	server *server.Server
	nrApp  *newrelic.Application
}

// NewTracingMiddleware constructs TracingMiddleware.
func NewTracingMiddleware(s *server.Server, nrApp *newrelic.Application) *TracingMiddleware {
	return &TracingMiddleware{
		server: s,
		nrApp:  nrApp,
	}
}

// NewRelicMiddleware returns the New Relic Echo middleware.
//
// What it does:
//   - If nrApp is nil, return a no-op middleware (passes request through unchanged).
//   - If nrApp exists, return nrecho.Middleware(tm.nrApp) which:
//   - starts a New Relic transaction for each request
//   - stores that transaction into request context
//   - records timing, status codes, etc.
//
// This middleware is what makes newrelic.FromContext(...) work later.
func (tm *TracingMiddleware) NewRelicMiddleware() echo.MiddlewareFunc {
	if tm.nrApp == nil {
		// No-op middleware: doesn't wrap the handler, just returns it.
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return next
		}
	}
	// Real New Relic middleware for Echo v4.
	return nrecho.Middleware(tm.nrApp)
}

// EnhanceTracing adds custom attributes to New Relic transactions.
//
// This middleware assumes NewRelicMiddleware() already ran earlier
// so that a transaction exists in request context.
//
// What it adds:
//   - client IP and user agent
//   - request id (if available)
//   - user id (if auth middleware set it)
//   - response status code (after handler)
//
// It also records errors using nrpkgerrors.Wrap so stack traces are nicer.
func (tm *TracingMiddleware) EnhanceTracing() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Grab New Relic transaction from request context.
			// This will be nil if:
			//   - New Relic is disabled
			//   - NewRelicMiddleware wasn't installed
			//   - middleware order is wrong
			txn := newrelic.FromContext(c.Request().Context())

			// If we don't have a transaction, do nothing and continue.
			if txn == nil {
				return next(c)
			}

			// Add some useful HTTP request attributes.
			// These show up in New Relic transaction traces as custom attributes.
			//
			// NOTE: Be careful: user agent can be huge and high-cardinality.
			txn.AddAttribute("http.real_ip", c.RealIP())
			txn.AddAttribute("http.user_agent", c.Request().UserAgent())

			// Add request ID if your RequestID middleware has set it.
			// This helps correlate New Relic traces with logs.
			if requestID := GetRequestID(c); requestID != "" {
				txn.AddAttribute("request.id", requestID)
			}

			// Add user id if auth middleware already put it into Echo context.
			// c.Get returns interface{}, so we check type.
			if userID := c.Get("user_id"); userID != nil {
				if userIDStr, ok := userID.(string); ok {
					txn.AddAttribute("user.id", userIDStr)
				}
			}

			// Run the handler (and rest of middleware chain).
			err := next(c)

			// If handler returns error:
			// - record it in New Relic
			// - wrap it with nrpkgerrors so stack trace info is captured
			//
			// IMPORTANT:
			// txn.NoticeError doesn't automatically stop Echo from handling the error.
			// You still return err so global error handler can respond properly.
			if err != nil {
				txn.NoticeError(nrpkgerrors.Wrap(err))
			}

			// Add response status code as an attribute.
			// This is captured after handler runs (since status is known then).
			txn.AddAttribute("http.status_code", c.Response().Status)

			return err
		}
	}
}
