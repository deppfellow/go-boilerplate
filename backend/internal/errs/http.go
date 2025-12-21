// Package errs define custom error types and utilities.
//
// Its purpose is to create specific error structures..
// (e.g. FieldErrors for forms or HTTPError for API responses)..
// to ensure the client receive meaningful, actionable, and consistent..
// error messages.
//
// - Return consistent error shapes to API clients (JSON).
// - Support field-level validation errors for forms.
// - Support "action hints" (like redirect) that frontends can interpret.
// - Provide errors that play nicely with Go's standard errors package.
package errs

import "strings"

// FieldError represents a field-level validation error (typical for forms).
// Example:
//
//	{ "field": "email", "error": "invalid email format" }
type FieldError struct {
	// Field is the field name/key the error relates to (e.g. "email").
	Field string `json:"field"`

	// Error is the human-readable error message.
	Error string `json:"error"`
}

// ActionType is a string-based enum describing what the client should do.
type ActionType string

const (
	// ActionTypeRedirect tells the client it should redirect somewhere.
	// Usually "Value" holds the URL or route.
	ActionTypeRedirect ActionType = "redirect"
)

// Action describes an optional “what the client should do next” instruction.
//
// This is not common in all APIs, but can be handy for auth flows:
// e.g. “redirect to login”.
type Action struct {
	// Type is the kind of action (e.g. "redirect").
	Type ActionType `json:"type"`

	// Message is human-readable guidance for the client/UI.
	Message string `json:"message"`

	// Value is the payload for the action (e.g. redirect URL).
	Value string `json:"value"`
}

// HTTPError is the main custom error type for API responses.
//
// It implements the `error` interface via Error().
// It is designed to be serialized directly to JSON.
// Fields:
//   - Code: machine-friendly error code (e.g. "BAD_REQUEST").
//   - Message: human-friendly message.
//   - Status: HTTP status code.
//   - Override: flag to let middleware decide whether to override the message.
//   - Errors: list of per-field errors (validation).
//   - Action: client instruction, action to be taken (optional).
type HTTPError struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Status   int    `json:"status"`
	Override bool   `json:"override"`

	// Errors holds field-level validation errors, typically for form inputs.
	Errors []FieldError `json:"errors"`

	/// Action is an optional client instruction (redirect, etc.).
	Action *Action `json:"action"`
}

// Error makes *HTTPError satisfy the built-in `error` interface.
//
// When you do `return err`, Go expects an error with method `Error() string`.
// Here it returns the Message, so printing/logging the error shows the message.
func (e *HTTPError) Error() string {
	return e.Message
}

// Is customizes how errors.Is(...) treats HTTPError.
//
// errors.Is(err, target) checks if err matches target.
// This implementation returns true if `target` is also a *HTTPError.
//
// Important nuance:
// This does NOT compare Code/Status/etc.
// It only checks whether the other thing is the same *type* (*HTTPError).
func (e *HTTPError) Is(target error) bool {
	// Type assertion:
	// - target.(*HTTPError) tries to treat target as *HTTPError.
	// - ok is true if the cast works.
	_, ok := target.(*HTTPError)

	return ok
}

// WithMessage returns a *copy* of this HTTPError with Message replaced.
//
// Useful if you have a base error template and want to customize message
// without mutating the original.
func (e *HTTPError) WithMessage(message string) *HTTPError {
	return &HTTPError{
		// Copy everything, replace only Message.
		Code:     e.Code,
		Message:  message,
		Status:   e.Status,
		Override: e.Override,
		Errors:   e.Errors,
		Action:   e.Action,
	}
}

// MakeUpperCaseWithUnderscores converts a string into an UPPER_CASE_WITH_UNDERSCORES format.
//
// Example:
//
//	"Bad Request" -> "BAD_REQUEST"
//
// Used to create stable machine-readable error codes from HTTP status text.
func MakeUpperCaseWithUnderscores(str string) string {
	// strings.ReplaceAll(str, " ", "_") replaces spaces with underscores.
	// strings.ToUpper(...) uppercases the whole result.
	return strings.ToUpper(strings.ReplaceAll(str, " ", "_"))
}
