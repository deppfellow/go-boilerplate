// Package validation contains the logic for validating
// request data.
//
// It uses the `validator` library to enforce rules (like
// required fields or email formats) defined in struct tags
// and extracts validation errors into a format the client can
// understand
package validation

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/deppfellow/go-boilerplate/internal/errs"
	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
)

// Validatable is implemented by request payload types that know how to validate themselves.
//
// Typical pattern:
// - Define a request struct with validator tags (`validate:"required,email"`)
// - Implement Validate() error that runs validator.Struct(req)
// - Return validator.ValidationErrors (or CustomValidationErrors for custom cases)
type Validatable interface {
	Validate() error
}

// CustomValidationError represents a single validation issue for a specific field.
// This is used for validation errors that cannot be expressed via validator tags.
type CustomValidationError struct {
	Field   string
	Message string
}

// CustomValidationErrors is a slice of custom validation errors that satisfies error.
type CustomValidationErrors []CustomValidationError

func (c CustomValidationErrors) Error() string {
	return "Validation failed"
}

// BindAndValidate binds request data into payload and validates it.
//
// Flow:
// 1) c.Bind(payload) populates request struct from the incoming request body/params.
// 2) payload.Validate() applies validation rules.
// 3) Returns *errs.HTTPError (400) with field-level errors if validation fails.
//
// NOTE: c.Bind expects a pointer to a struct. If payload is not a pointer,
// binding will fail or behave unexpectedly.
func BindAndValidate(c echo.Context, payload Validatable) error {
	// Bind request body into payload.
	// Echo returns an error when JSON is malformed or types mismatch.
	if err := c.Bind(payload); err != nil {
		// This parsing is brittle: it depends on Echo's bind error formatting.
		// Consider replacing this with a safer parser or a fixed message if needed.
		message := strings.Split(strings.Split(err.Error(), ",")[1], "message=")[1]
		return errs.NewBadRequestError(message, false, nil, nil, nil)
	}

	// Validate struct and return field errors if any.
	if msg, fieldErrors := validateStruct(payload); fieldErrors != nil {
		return errs.NewBadRequestError(msg, true, nil, fieldErrors, nil)
	}

	return nil
}

// validateStruct calls v.Validate() and extracts field errors if validation fails.
func validateStruct(v Validatable) (string, []errs.FieldError) {
	if err := v.Validate(); err != nil {
		return extractValidationError(err)
	}
	return "", nil
}

func extractValidationError(err error) (string, []errs.FieldError) {
	var fieldErrors []errs.FieldError

	// validator.ValidationErrors is returned when struct tag validation fails.
	validationErrors, ok := err.(validator.ValidationErrors)
	if !ok {
		// Custom validation errors: convert directly.
		customValidationErrors := err.(CustomValidationErrors)
		for _, err := range customValidationErrors {
			fieldErrors = append(fieldErrors, errs.FieldError{
				Field: err.Field,
				Error: err.Message,
			})
		}

		// No return here is necessary: validationErrors is nil, loop below won’t run.
	}

	// Convert validator.ValidationErrors into user-friendly messages.
	for _, err := range validationErrors {
		field := strings.ToLower(err.Field())
		var msg string

		switch err.Tag() {
		case "required":
			msg = "is required"

		case "min":
			// min tag means:
			// - for strings: minimum length
			// - for numbers: minimum value
			if err.Type().Kind() == reflect.String {
				msg = fmt.Sprintf("must be at least %s characters", err.Param())
			} else {
				msg = fmt.Sprintf("must be at least %s", err.Param())
			}

		case "max":
			// max tag means:
			// - for strings: maximum length
			// - for numbers: maximum value
			if err.Type().Kind() == reflect.String {
				msg = fmt.Sprintf("must not exceed %s characters", err.Param())
			} else {
				msg = fmt.Sprintf("must not exceed %s", err.Param())
			}

		case "oneof":
			msg = fmt.Sprintf("must be one of: %s", err.Param())

		case "email":
			msg = "must be a valid email address"

		case "e164":
			msg = "must be a valid phone number with country code"

		case "uuid":
			msg = "must be a valid UUID"

		case "uuidList":
			msg = "must be a comma-separated list of valid UUIDs"

		case "dive":
			// dive is used when validating slices/arrays and one of the nested items fails.
			msg = "some items are invalid"

		default:
			// Fallback for tags not explicitly handled above.
			// Includes tag name and param (if any) to help debugging.
			if err.Param() != "" {
				msg = fmt.Sprintf("%s: %s:%s", field, err.Tag(), err.Param())
			} else {
				msg = fmt.Sprintf("%s: %s", field, err.Tag())
			}
		}

		fieldErrors = append(fieldErrors, errs.FieldError{
			Field: strings.ToLower(err.Field()),
			Error: msg,
		})
	}

	return "Validation failed", fieldErrors
}

// uuidRegex matches standard UUID format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsValidUUID checks whether a string matches UUID format.
//
// Note: This validates format only. It does not validate UUID version/variant semantics.
func IsValidUUID(uuid string) bool {
	return uuidRegex.MatchString(uuid)
}
