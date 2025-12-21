package middleware

import (
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const (
	// RequestIDHeader is the HTTP header used to store the request correlation ID.
	// Many systems use X-Request-ID or X-Correlation-ID.
	RequestIDHeader = "X-Request-ID"

	// RequestIDKey is the internal key used to store the ID in Echo context.
	RequestIDKey = "request_id"
)

// RequestID returns an Echo middleware that ensures each request has a request ID.
//
// Behavior:
//   - If incoming request already has X-Request-ID header: reuse it.
//   - If not: generate a new UUID.
//   - Store it in Echo context (c.Set) for internal access.
//   - Set it on response header so clients can see it too.
func RequestID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Get request ID from incoming header (if any).
			requestID := c.Request().Header.Get(RequestIDHeader)

			// If not provided upstream, generate a UUID.
			// UUIDs are cheap, unique enough, and easy for log correlation.
			if requestID == "" {
				requestID = uuid.New().String()
			}

			// Store in Echo context so other middleware/handlers can read it.
			c.Set(RequestIDKey, requestID)

			// Echo it back in response header so:
			// - client can report it in bug reports
			// - reverse proxies/log systems can correlate
			c.Response().Header().Set(RequestIDHeader, requestID)

			// Continue request pipeline.
			return next(c)
		}
	}
}

// GetRequestID retrieves the request ID from Echo context.
//
// Returns empty string if not set.
func GetRequestID(c echo.Context) string {
	if requestID, ok := c.Get(RequestIDKey).(string); ok {
		return requestID
	}
	return ""
}
