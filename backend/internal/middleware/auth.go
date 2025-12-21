package middleware

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	clerkhttp "github.com/clerk/clerk-sdk-go/v2/http"
	"github.com/deppfellow/go-boilerplate/internal/errs"
	"github.com/deppfellow/go-boilerplate/internal/server"
	"github.com/labstack/echo/v4"
)

// AuthMiddleware holds the app Server so middleware can access shared deps
// like Logger and Config.
type AuthMiddleware struct {
	server *server.Server
}

// NewAuthMiddleware constructs an AuthMiddleware.
func NewAuthMiddleware(s *server.Server) *AuthMiddleware {
	return &AuthMiddleware{
		server: s,
	}
}

// RequireAuth is an Echo middleware that enforces authentication using Clerk.
//
// High-level behavior:
//  1. It wraps Clerk's middleware that parses the Authorization header.
//  2. If Clerk fails auth, it uses AuthorizationFailureHandler to emit a JSON 401.
//  3. If Clerk succeeds, it extracts session claims from request context.
//  4. It stores useful values into Echo context (user_id, role, permissions).
//  5. It calls the next handler.
func (auth *AuthMiddleware) RequireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	// echo.WrapMiddleware converts a standard net/http middleware to Echo middleware.
	//
	// clerkhttp.WithHeaderAuthorization is Clerk's middleware that:
	// - reads Authorization: Bearer <token>
	// - validates it
	// - populates the request context with Clerk session claims
	return echo.WrapMiddleware(
		clerkhttp.WithHeaderAuthorization(
			// AuthorizationFailureHandler is called when token is missing/invalid.
			clerkhttp.AuthorizationFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				start := time.Now()

				// Return JSON.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)

				// Construct a basic response map.
				//
				// NOTE: override/status are strings here, not bool/int.
				// That’s inconsistent with your HTTPError struct.
				response := map[string]string{
					"code":     "UNAUTHORIZED",
					"message":  "Unauthorized",
					"override": "false",
					"status":   "401",
				}

				// Write the JSON response.
				if err := json.NewEncoder(w).Encode(response); err != nil {
					// If writing response fails, log it with duration.
					auth.server.Logger.Error().
						Err(err).
						Str("function", "RequireAuth").
						Dur("duration", time.Since(start)).
						Msg("failed to write JSON response")
				} else {
					// This message is misleading: auth failure handler is called for auth failures,
					// not necessarily "could not get session claims".
					auth.server.Logger.Error().
						Str("function", "RequireAuth").
						Dur("duration", time.Since(start)).
						Msg("could not get session claims from context")
				}
			}))))(
		// This function runs if Clerk middleware let the request through.
		func(c echo.Context) error {
			start := time.Now()

			// Clerk middleware puts session claims into the request context.
			// You retrieve them using clerk.SessionClaimsFromContext(ctx).
			claims, ok := clerk.SessionClaimsFromContext(c.Request().Context())
			if !ok {
				// If claims aren't present, treat as unauthenticated.
				auth.server.Logger.Error().
					Str("function", "RequireAuth").
					Str("request_id", GetRequestID(c)). // correlation ID from request_id middleware
					Dur("duration", time.Since(start)).
					Msg("could not get session claims from context")

				return errs.NewUnauthorizedError("Unauthorized", false)
			}

			// Store auth values into Echo context for handlers to read later.
			//
			// These are NOT stored in Go's context.Context.
			// They're stored in Echo's context (a request-scoped key/value bag).
			c.Set("user_id", claims.Subject)
			c.Set("user_role", claims.ActiveOrganizationRole)
			c.Set("permissions", claims.Claims.ActiveOrganizationPermissions)

			// Success log with request_id for traceability.
			auth.server.Logger.Info().
				Str("function", "RequireAuth").
				Str("user_id", claims.Subject).
				Str("request_id", GetRequestID(c)).
				Dur("duration", time.Since(start)).
				Msg("user authenticated successfully")

			// Continue to next handler.
			return next(c)
		})
}
