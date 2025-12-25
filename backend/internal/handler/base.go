package handler

import (
	"time"

	"github.com/deppfellow/go-boilerplate/internal/middleware"
	"github.com/deppfellow/go-boilerplate/internal/server"
	"github.com/deppfellow/go-boilerplate/internal/validation"
	"github.com/labstack/echo/v4"
	"github.com/newrelic/go-agent/v3/integrations/nrpkgerrors"
	"github.com/newrelic/go-agent/v3/newrelic"
)

// Handler is the base handler type that holds shared application dependencies.
//
// It is embedded/used by concrete handlers (e.g., AuthHandler, HealthHandler) so they can
// access shared resources via *server.Server (config, logger, db, redis, job, etc.).
type Handler struct {
	server *server.Server
}

// NewHandler constructs a base Handler.
//
// Note: it returns the struct by value. This is fine because the struct only contains
// a pointer field (*server.Server). Copying it is cheap and still points to the same Server.
func NewHandler(s *server.Server) Handler {
	return Handler{server: s}
}

// --- Generic typed handler plumbing -----------------------------------------

// HandlerFunc represents a typed endpoint function that:
//
// - receives a validated request payload (Req)
// - returns a response (Res) or an error
//
// Req must satisfy validation.Validatable.
// In practice Req is typically a POINTER type, e.g. *CreateUserRequest,
// because Echo's Bind requires a pointer to populate fields.
type HandlerFunc[Req validation.Validatable, Res any] func(c echo.Context, req Req) (Res, error)

// HandlerFuncNoContent is a typed endpoint function for routes that return no response body
// (e.g., 204 No Content).
type HandlerFuncNoContent[Req validation.Validatable] func(c echo.Context, req Req) error

// ResponseHandler defines the interface for handling different response types
//
// It defines how a successful handler result is written to the HTTP response,
// and how observability attributes should be attached for that response type.
type ResponseHandler interface {
	// Handle writes the HTTP response for the given result.
	Handle(c echo.Context, result interface{}) error

	// GetOperation returns an operation name used for structured logging.
	// This helps distinguish handler types (json/no_content/file) in logs.
	GetOperation() string

	// AddAttributes attaches New Relic attributes based on response type and/or result.
	// This allows customization beyond the generic tracing middleware.
	AddAttributes(txn *newrelic.Transaction, result interface{})
}

// JSONResponseHandler writes JSON responses with a given status code.
type JSONResponseHandler struct {
	status int
}

func (h JSONResponseHandler) Handle(c echo.Context, result interface{}) error {
	return c.JSON(h.status, result)
}

func (h JSONResponseHandler) GetOperation() string {
	return "handler"
}

func (h JSONResponseHandler) AddAttributes(txn *newrelic.Transaction, result interface{}) {
	// http.status_code is already set by tracing middleware (EnhanceTracing).
}

// NoContentResponseHandler handles no-content responses
// It writes responses with no body (typically 204).
type NoContentResponseHandler struct {
	status int
}

func (h NoContentResponseHandler) Handle(c echo.Context, result interface{}) error {
	return c.NoContent(h.status)
}

func (h NoContentResponseHandler) GetOperation() string {
	return "handler_no_content"
}

func (h NoContentResponseHandler) AddAttributes(txn *newrelic.Transaction, result interface{}) {
	// http.status_code is already set by tracing middleware
}

// FileResponseHandler writes a file download response.
//
// It expects the handler result to be a []byte.
type FileResponseHandler struct {
	status      int
	filename    string
	contentType string
}

func (h FileResponseHandler) Handle(c echo.Context, result interface{}) error {
	// The contract for FileResponseHandler is: handler must return []byte.
	// If it's not []byte, this will panic; keep the contract tight.
	data := result.([]byte)

	// Force download via Content-Disposition.
	c.Response().Header().Set("Content-Disposition", "attachment; filename="+h.filename)

	// Force download via Content-Disposition.
	return c.Blob(h.status, h.contentType, data)
}

func (h FileResponseHandler) GetOperation() string {
	return "handler_file"
}

func (h FileResponseHandler) AddAttributes(txn *newrelic.Transaction, result interface{}) {
	if txn != nil {
		// http.status_code is already set by tracing middleware (EnhanceTracing).
		txn.AddAttribute("file.name", h.filename)
		txn.AddAttribute("file.content_type", h.contentType)
		if data, ok := result.([]byte); ok {
			txn.AddAttribute("file.size_bytes", len(data))
		}
	}
}

// handleRequest is the unified handler function that eliminates code duplication
//
// It is the shared execution pipeline for all handlers.
// It eliminates endpoint boilerplate by centralizing:
//
// - request binding + validation
// - structured logging (with request context)
// - New Relic tracing attributes and error reporting
// - timing metrics (validation duration, handler duration, total duration)
// - response writing (json / no-content / file)
//
// Req must satisfy validation.Validatable (usually pointer-to-struct).
func handleRequest[Req validation.Validatable](
	c echo.Context,
	req Req,
	handler func(c echo.Context, req Req) (interface{}, error),
	responseHandler ResponseHandler,
) error {
	start := time.Now()
	method := c.Request().Method
	path := c.Path()
	route := path

	// New Relic transaction is set by the New Relic Echo middleware (nrecho).
	txn := newrelic.FromContext(c.Request().Context())
	if txn != nil {
		// Attach handler name/route for easier filtering in New Relic.
		txn.AddAttribute("handler.name", route)

		// http.method and http.route are typically already set by nrecho middleware.
		// Allow response handlers to attach static attributes early (if any).
		responseHandler.AddAttributes(txn, nil)
	}

	// Get context-enhanced logger
	//
	// Use the context-enhanced logger set by ContextEnhancer middleware.
	// This logger should already include correlation fields (request_id, user_id, trace ids).
	loggerBuilder := middleware.GetLogger(c).With().
		Str("operation", responseHandler.GetOperation()).
		Str("method", method).
		Str("path", path).
		Str("route", route)

	// Add file-specific fields to logger if it's a file handler
	//
	// If response is a file download, include file metadata in logs.
	// Note: responseHandler is an interface; this asserts the concrete value type.
	if fileHandler, ok := responseHandler.(FileResponseHandler); ok {
		loggerBuilder = loggerBuilder.
			Str("filename", fileHandler.filename).
			Str("content_type", fileHandler.contentType)
	}

	logger := loggerBuilder.Logger()

	// user.id is already set by tracing middleware

	logger.Info().Msg("handling request")

	// ---------------- Validation phase ---------------------------------------
	// Validation with observability
	validationStart := time.Now()

	// BindAndValidate does:
	// - c.Bind(payload) to populate req
	// - payload.Validate() which uses validator tags or custom validations
	//
	// IMPORTANT: req should be a pointer type so c.Bind can mutate it.
	if err := validation.BindAndValidate(c, req); err != nil {
		validationDuration := time.Since(validationStart)

		logger.Error().
			Err(err).
			Dur("validation_duration", validationDuration).
			Msg("request validation failed")

		// Report validation errors to New Relic as noticed errors.
		if txn != nil {
			txn.NoticeError(nrpkgerrors.Wrap(err))
			txn.AddAttribute("validation.status", "failed")
			txn.AddAttribute("validation.duration_ms", validationDuration.Milliseconds())
		}

		// Return error to let global error handler format the response.
		return err
	}

	validationDuration := time.Since(validationStart)
	if txn != nil {
		txn.AddAttribute("validation.status", "success")
		txn.AddAttribute("validation.duration_ms", validationDuration.Milliseconds())
	}

	logger.Debug().
		Dur("validation_duration", validationDuration).
		Msg("request validation successful")

	// ---------------- Handler execution phase --------------------------------
	// Execute handler with observability
	handlerStart := time.Now()
	result, err := handler(c, req)
	handlerDuration := time.Since(handlerStart)

	if err != nil {
		totalDuration := time.Since(start)

		logger.Error().
			Err(err).
			Dur("handler_duration", handlerDuration).
			Dur("total_duration", totalDuration).
			Msg("handler execution failed")

		if txn != nil {
			txn.NoticeError(nrpkgerrors.Wrap(err))
			txn.AddAttribute("handler.status", "error")
			txn.AddAttribute("handler.duration_ms", handlerDuration.Milliseconds())
			txn.AddAttribute("total.duration_ms", totalDuration.Milliseconds())
		}
		return err
	}

	totalDuration := time.Since(start)

	// Record success attributes for tracing/metrics.
	if txn != nil {
		txn.AddAttribute("handler.status", "success")
		txn.AddAttribute("handler.duration_ms", handlerDuration.Milliseconds())
		txn.AddAttribute("total.duration_ms", totalDuration.Milliseconds())

		// Let response handler attach attributes that depend on the response payload.
		responseHandler.AddAttributes(txn, result)
	}

	logger.Info().
		Dur("handler_duration", handlerDuration).
		Dur("validation_duration", validationDuration).
		Dur("total_duration", totalDuration).
		Msg("request completed successfully")

	// Write the response using the configured response handler.
	return responseHandler.Handle(c, result)
}

// Handle wraps a handler with validation, error handling, logging, metrics, and tracing
//
// It returns an echo.HandlerFunc so it can be registered directly on routes.
//
// Usage pattern (typical):
//
//	router.POST("/x", handler.Handle(h, myHandlerFn, http.StatusCreated, &MyReq{}))
func Handle[Req validation.Validatable, Res any](
	h Handler,
	handler HandlerFunc[Req, Res],
	status int,
	req Req,
) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Adapt typed handler (Res) into the generic interface{} pipeline.
		return handleRequest(c, req, func(c echo.Context, req Req) (interface{}, error) {
			return handler(c, req)
		}, JSONResponseHandler{status: status})
	}
}

// HandleFile wraps a handler that returns file bytes ([]byte) into the unified pipeline.
//
// It sets response headers (Content-Disposition) and writes Blob response.
func HandleFile[Req validation.Validatable](
	h Handler,
	handler HandlerFunc[Req, []byte],
	status int,
	req Req,
	filename string,
	contentType string,
) echo.HandlerFunc {
	return func(c echo.Context) error {
		return handleRequest(c, req, func(c echo.Context, req Req) (interface{}, error) {
			return handler(c, req)
		}, FileResponseHandler{
			status:      status,
			filename:    filename,
			contentType: contentType,
		})
	}
}

// HandleNoContent wraps a handler with validation, error handling, logging, metrics, and tracing for endpoints that don't return content
//
// Intended for endpoints that return no body (e.g., DELETE success with 204).
func HandleNoContent[Req validation.Validatable](
	h Handler,
	handler HandlerFuncNoContent[Req],
	status int,
	req Req,
) echo.HandlerFunc {
	return func(c echo.Context) error {
		return handleRequest(c, req, func(c echo.Context, req Req) (interface{}, error) {
			err := handler(c, req)
			return nil, err
		}, NoContentResponseHandler{status: status})
	}
}
