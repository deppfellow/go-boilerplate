package errs

import (
	"net/http"
)

// NewUnauthorizedError creates a 401 Unauthorized HTTPError.
//
// Parameters:
//   - message: text to send to client
//   - override: a flag your middleware/handler can use to decide whether
//     to replace the message (for security reasons in prod, for example).
func NewUnauthorizedError(message string, override bool) *HTTPError {
	return &HTTPError{
		// http.StatusText(401) => "Unauthorized"
		// MakeUpperCaseWithUnderscores => "UNAUTHORIZED"
		Code:     MakeUpperCaseWithUnderscores(http.StatusText(http.StatusUnauthorized)),
		Message:  message,
		Status:   http.StatusUnauthorized,
		Override: override,
	}
}

// NewForbiddenError creates a 403 Forbidden HTTPError.
func NewForbiddenError(message string, override bool) *HTTPError {
	return &HTTPError{
		// http.StatusText(403) => "Forbidden" => "FORBIDDEN"
		Code:     MakeUpperCaseWithUnderscores(http.StatusText(http.StatusForbidden)),
		Message:  message,
		Status:   http.StatusForbidden,
		Override: override,
	}
}

// NewBadRequestError creates a 400 Bad Request HTTPError.
//
// This supports extra payload:
//   - code: optional custom code string (if nil, defaults to "BAD_REQUEST")
//   - errors: optional slice of field errors (validation errors)
//   - action: optional client instruction (e.g. redirect)
//
// This is designed for form validation and “you sent garbage” cases.
func NewBadRequestError(message string, override bool, code *string, errors []FieldError, action *Action) *HTTPError {
	// Default code comes from HTTP status text:
	// http.StatusText(400) => "Bad Request" => "BAD_REQUEST"
	formattedCode := MakeUpperCaseWithUnderscores(http.StatusText(http.StatusBadRequest))

	// If caller supplies custom code pointer, use it.
	// Note: this assumes the caller already formatted it the way they want.
	if code != nil {
		formattedCode = *code
	}

	return &HTTPError{
		Code:     formattedCode,
		Message:  message,
		Status:   http.StatusBadRequest,
		Override: override,
		Errors:   errors,
		Action:   action,
	}
}

// NewNotFoundError creates a 404 Not Found HTTPError.
//
// Supports optional custom code override similar to NewBadRequestError.
func NewNotFoundError(message string, override bool, code *string) *HTTPError {
	// Default code: "NOT_FOUND"
	formattedCode := MakeUpperCaseWithUnderscores(http.StatusText(http.StatusNotFound))

	// Optional custom error code.
	if code != nil {
		formattedCode = *code
	}

	return &HTTPError{
		Code:     formattedCode,
		Message:  message,
		Status:   http.StatusNotFound,
		Override: override,
	}
}

// NewInternalServerError creates a 500 Internal Server Error HTTPError.
//
// Note:
//   - message is the generic status text, not the real internal error message.
//   - this is a security-friendly default: clients don’t need your stack traces.
//   - Override is false by default: you usually don't want to override generic 500s.
func NewInternalServerError() *HTTPError {
	return &HTTPError{
		Code:     MakeUpperCaseWithUnderscores(http.StatusText(http.StatusInternalServerError)),
		Message:  http.StatusText(http.StatusInternalServerError),
		Status:   http.StatusInternalServerError,
		Override: false,
	}
}

// ValidationError converts a generic validation error into a 400 Bad Request HTTPError.
//
// This is a helper so you can do:
//
//	return errs.ValidationError(err)
//
// and clients get consistent error structure.
func ValidationError(err error) *HTTPError {
	// Builds a message like: "Validation failed: <validator message>"
	// and returns a 400 with that message.
	return NewBadRequestError("Validation failed: "+err.Error(), false, nil, nil, nil)
}
