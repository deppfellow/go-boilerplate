package middleware

import (
	"context"

	"github.com/deppfellow/go-boilerplate/internal/logger"
	"github.com/deppfellow/go-boilerplate/internal/server"
	"github.com/labstack/echo/v4"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/rs/zerolog"
)

const (
	// UserIDKey and UserRoleKey are intended to be the canonical keys used
	// to store and retrieve user identity from Echo context.
	//
	// NOTE: In this file you never actually set these keys, you set "user_id"
	// elsewhere in auth middleware. That’s slightly inconsistent.
	UserIDKey   = "user_id"
	UserRoleKey = "user_role"

	// LoggerKey is used as the key for storing the request-scoped logger.
	LoggerKey = "logger"
)

// ContextEnhancer is a middleware helper that enriches request context.
//
// It builds a request-scoped logger with useful fields like:
//   - request_id
//   - method, path, ip
//   - trace.id/span.id (if New Relic transaction exists)
//   - user_id/user_role (if auth middleware set them)
//
// It then stores that logger in:
//   - Echo context (c.Set)
//   - Go request context (context.WithValue)
type ContextEnhancer struct {
	server *server.Server
}

// NewContextEnhancer creates a new ContextEnhancer using the app Server container.
func NewContextEnhancer(s *server.Server) *ContextEnhancer {
	return &ContextEnhancer{server: s}
}

// EnhanceContext returns an Echo middleware.
//
// For every request, it:
//  1. gets the request ID (from request_id middleware)
//  2. creates a logger with request fields
//  3. adds trace context if available (New Relic)
//  4. adds user context if available (from auth middleware)
//  5. stores that logger in Echo context + Go context
func (ce *ContextEnhancer) EnhanceContext() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Extract request ID (from your RequestID middleware).
			// If RequestID middleware didn't run before this, requestID may be "".
			requestID := GetRequestID(c)

			// Create a child logger that includes request-related fields.
			//
			// ce.server.Logger.With() starts a "logger builder".
			// Str(...) adds structured fields.
			// Logger() finalizes a new logger instance.
			contextLogger := ce.server.Logger.With().
				Str("request_id", requestID).
				Str("method", c.Request().Method).
				Str("path", c.Path()). // Echo route path template (e.g. "/users/:id"), not raw URL
				Str("ip", c.RealIP()). // Uses X-Forwarded-For etc when configured
				Logger()

			// Add New Relic trace context if a transaction exists in request context.
			//
			// newrelic.FromContext(ctx) returns the txn set by New Relic middleware/integration.
			// logger.WithTraceContext adds trace.id + span.id to logger fields.
			if txn := newrelic.FromContext(c.Request().Context()); txn != nil {
				contextLogger = logger.WithTraceContext(contextLogger, txn)
			}

			// Extract user_id from Echo context if auth middleware has already set it.
			// If auth middleware runs after this enhancer, it will not find user info here.
			if userID := ce.extractUserID(c); userID != "" {
				// contextLogger.With() creates another logger builder with existing fields
				// + new user_id field.
				contextLogger = contextLogger.With().Str("user_id", userID).Logger()
			}

			// Same idea for role.
			if userRole := ce.extractUserRole(c); userRole != "" {
				contextLogger = contextLogger.With().Str("user_role", userRole).Logger()
			}

			// Store the enhanced logger in Echo context.
			//
			// IMPORTANT: You store *&contextLogger (pointer) so handlers can retrieve it.
			c.Set(LoggerKey, &contextLogger)

			// ALSO store the logger pointer into the Go request context.
			//
			// This allows non-Echo code (that only sees context.Context)
			// to fetch the request logger, e.g. in DB/repo layers.
			//
			// NOTE: context.WithValue is fine for request-scoped values,
			// but you should normally use a custom type for the key instead of string
			// to avoid collisions across packages.
			ctx := context.WithValue(c.Request().Context(), LoggerKey, &contextLogger)

			// Replace the request with a new request that has the enriched context.
			c.SetRequest(c.Request().WithContext(ctx))

			// Continue the middleware chain.
			return next(c)
		}
	}
}

// extractUserID extracts user_id from Echo context.
//
// It expects auth middleware to have already done:
//
//	c.Set("user_id", claims.Subject)
func (ce *ContextEnhancer) extractUserID(c echo.Context) string {
	// Type assertion: try to get "user_id" as a string.
	if userID, ok := c.Get("user_id").(string); ok && userID != "" {
		return userID
	}
	return ""
}

// extractUserRole extracts user_role from Echo context.
//
// It expects auth middleware to have already done:
//
//	c.Set("user_role", claims.ActiveOrganizationRole)
func (ce *ContextEnhancer) extractUserRole(c echo.Context) string {
	if userRole, ok := c.Get("user_role").(string); ok && userRole != "" {
		return userRole
	}
	return ""
}

// GetUserID reads user_id from Echo context using UserIDKey.
//
// NOTE: This function is redundant with extractUserID.
// Also: it depends on c.Set(UserIDKey, ...) being used consistently.
// In your auth middleware you set "user_id" directly, which matches UserIDKey,
// so it works.
func GetUserID(c echo.Context) string {
	if userID, ok := c.Get(UserIDKey).(string); ok {
		return userID
	}
	return ""
}

// GetLogger retrieves the request-scoped logger from Echo context.
//
// If EnhanceContext middleware didn't run, it returns a no-op logger.
func GetLogger(c echo.Context) *zerolog.Logger {
	// Try to pull *zerolog.Logger stored under LoggerKey.
	if logger, ok := c.Get(LoggerKey).(*zerolog.Logger); ok {
		return logger
	}

	// Fallback: return a logger that discards output.
	// This prevents nil pointer crashes, but also hides logs if misconfigured.
	logger := zerolog.Nop()
	return &logger
}
