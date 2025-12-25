package middleware

import (
	"net/http"

	"github.com/deppfellow/go-boilerplate/internal/errs"
	"github.com/deppfellow/go-boilerplate/internal/server"
	"github.com/deppfellow/go-boilerplate/internal/sqlerr"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

// GlobalMiddlewares groups “global” middleware and the global error handler.
//
// Why a struct?
//   - So middleware functions can access shared app dependencies from *server.Server,
//     especially config and observability/logging stuff.
type GlobalMiddlewares struct {
	server *server.Server
}

// NewGlobalMiddlewares constructs the middleware bundle.
// It stores a pointer to your application container (*server.Server) so all global
// middleware can read config values (CORS origins, env, etc.) and services if needed.
func NewGlobalMiddlewares(s *server.Server) *GlobalMiddlewares {
	return &GlobalMiddlewares{
		server: s,
	}
}

// CORS returns Echo’s CORS middleware configured by your server config.
//
// It allows browser-based clients to call your API from specific origins.
// If CORSAllowedOrigins is wrong, your frontend will “mysteriously” fail.
func (global *GlobalMiddlewares) CORS() echo.MiddlewareFunc {
	return middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: global.server.Config.Server.CORSAllowedOrigins,
	})
}

// RequestLogger returns Echo’s request logger middleware, but with a custom LogValuesFunc.
//
// Why custom?
//   - You want structured logs via zerolog
//   - You want correlation fields (request_id, user_id)
//   - You want correct status codes even when the handler returns an error and the global
//     error handler sets the final response later.
//
// Tutor intent (matches the code + the usual Echo behavior):
// - request logging should produce one “API” log line per request, with severity based on status.
func (global *GlobalMiddlewares) RequestLogger() echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:     true,
		LogStatus:  true,
		LogError:   true,
		LogLatency: true,
		LogHost:    true,
		LogMethod:  true,
		LogURIPath: true,

		// LogValuesFunc is called at the end of request handling.
		// v contains measured request metadata: latency, status, error, etc.
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			statusCode := v.Status

			// IMPORTANT QUIRK:
			// When a handler returns an error, Echo may not have written the final status yet.
			// Your GlobalErrorHandler will decide the final status and write the response.
			//
			// Therefore, if v.Error != nil, we try to derive the status from the error type.
			// This avoids logging status=200 for an error request.
			// Reference: https://github.com/labstack/echo/issues/2310#issuecomment-1288196898
			if v.Error != nil {
				var httpErr *errs.HTTPError
				var echoErr *echo.HTTPError

				if errors.As(v.Error, &httpErr) {
					// Our custom error knows its intended status.
					statusCode = httpErr.Status
				} else if errors.As(v.Error, &echoErr) {
					// Echo’s error type stores its status in Code.
					statusCode = echoErr.Code
				}
			}

			// Pull the enhanced request logger from context.
			// ContextEnhancer middleware should have stored this already.
			logger := GetLogger(c)

			// Pick log level based on status:
			// - 5xx = server fault -> Error
			// - 4xx = client fault -> Warn
			// - otherwise -> Info
			var e *zerolog.Event
			switch {
			case statusCode >= 500:
				e = logger.Error().Err(v.Error)
			case statusCode >= 400:
				e = logger.Warn()
			default:
				e = logger.Info()
			}

			// Correlation: request id (if RequestID middleware ran).
			if requestID := GetRequestID(c); requestID != "" {
				e = e.Str("request_id", requestID)
			}

			// Correlation: user id (if auth middleware ran and set it).
			if userID := GetUserID(c); userID != "" {
				e = e.Str("user_id", userID)
			}

			// Add the standard request log fields.
			e.
				Dur("latency", v.Latency).
				Int("status", statusCode).
				Str("method", v.Method).
				Str("uri", v.URI).
				Str("host", v.Host).
				Str("ip", c.RealIP()).
				Str("user_agent", c.Request().UserAgent()).
				Msg("API")

			return nil
		},
	})
}

// Recover returns Echo’s panic recovery middleware.
//
// If your handler panics, Recover prevents the whole process from crashing.
// Panics become 500 responses (and should be logged).
func (global *GlobalMiddlewares) Recover() echo.MiddlewareFunc {
	return middleware.Recover()
}

// Secure returns Echo’s secure headers middleware.
//
// Adds standard security-related headers.
// It’s not a magical shield, but it stops some low-effort nonsense.
func (global *GlobalMiddlewares) Secure() echo.MiddlewareFunc {
	return middleware.Secure()
}

// GlobalErrorHandler is the final error funnel for the entire HTTP server.
//
// Tutor explanation (02:44:06+):
// - Every error ends up here, regardless of where it happened.
// - Here you choose how to translate it into a clean response for the client.
// - You can log using the request-scoped context logger.
func (global *GlobalMiddlewares) GlobalErrorHandler(err error, c echo.Context) {
	// Keep the original error for logging.
	// We may replace `err` with a friendlier/sanitized error for the client,
	// but logs should keep the real underlying error for debugging.
	originalErr := err

	// If error is not already our custom HTTP error, attempt to classify/convert it.
	var httpErr *errs.HTTPError
	if !errors.As(err, &httpErr) {

		// Echo also has its own HTTPError type.
		// Tutor notes: the major remaining echo error you care about is route 404.
		var echoErr *echo.HTTPError
		if errors.As(err, &echoErr) {
			// If the user hits a route that doesn’t exist:
			// convert it into your own NotFound shape.
			if echoErr.Code == http.StatusNotFound {
				err = errs.NewNotFoundError("Route not found", false, nil)
			}

		} else {
			// Otherwise treat it as a likely “driver / database / unknown” error.
			// sqlerr.HandleError converts pgx/pgconn/sql errors into application HTTP errors,
			// e.g. unique violation -> 400 with a friendly message.
			err = sqlerr.HandleError(err)
		}
	}

	// Now map whichever error we ended up with into response fields.
	var echoErr *echo.HTTPError
	var status int
	var code string
	var message string
	var fieldErrors []errs.FieldError
	var action *errs.Action

	switch {
	case errors.As(err, &httpErr):
		// Our custom error already has the full response schema.
		status = httpErr.Status
		code = httpErr.Code
		message = httpErr.Message
		fieldErrors = httpErr.Errors
		action = httpErr.Action

	case errors.As(err, &echoErr):
		// Convert Echo’s error into your schema.
		status = echoErr.Code
		code = errs.MakeUpperCaseWithUnderscores(http.StatusText(status))

		// Echo error message can be a string or any type; normalize it to string.
		if msg, ok := echoErr.Message.(string); ok {
			message = msg
		} else {
			message = http.StatusText(echoErr.Code)
		}

	default:
		// Absolute fallback: safe 500.
		status = http.StatusInternalServerError
		code = errs.MakeUpperCaseWithUnderscores(http.StatusText(http.StatusInternalServerError))
		message = http.StatusText(http.StatusInternalServerError)
	}

	// Log the original error for debugging.
	// Use enhanced logger from context (request_id/user/trace already included by other middleware).
	logger := *GetLogger(c)

	logger.Error().Stack().
		Err(originalErr).
		Int("status", status).
		Str("error_code", code).
		Msg(message)

	// Only write response if it hasn’t already been written.
	if !c.Response().Committed {
		_ = c.JSON(status, errs.HTTPError{
			Code:     code,
			Message:  message,
			Status:   status,
			Override: httpErr != nil && httpErr.Override,
			Errors:   fieldErrors,
			Action:   action,
		})
	}
}
