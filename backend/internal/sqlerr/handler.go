package sqlerr

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/deppfellow/go-boilerplate/internal/errs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// ErrCode reports the mapped sqlerr.Code for a given error.
//
// Behavior:
//   - If err can be unwrapped into *sqlerr.Error, return its Code.
//   - Otherwise return sqlerr.Other.
//
// This is useful if you already converted/normalized errors into sqlerr.Error
// and want to quickly check their category.
func ErrCode(err error) Code {
	var pgerr *Error
	// errors.As walks the error chain (using Unwrap()) and tries to find *Error.
	if errors.As(err, &pgerr) {
		return pgerr.Code
	}
	return Other
}

// ConvertPgError converts a pgconn.PgError (raw Postgres error) into our custom sqlerr.Error.
//
// pgconn.PgError contains Postgres-specific fields like:
//   - Code (SQLSTATE)
//   - Severity
//   - TableName/ColumnName/ConstraintName etc.
//
// We map SQLSTATE + Severity into our enums for easier switching.
func ConvertPgError(src *pgconn.PgError) *Error {
	return &Error{
		Code:           MapCode(src.Code),         // map SQLSTATE to friendly code enum
		Severity:       MapSeverity(src.Severity), // map severity string to enum
		DatabaseCode:   src.Code,                  // keep original SQLSTATE
		Message:        src.Message,               // DB's main message
		SchemaName:     src.SchemaName,
		TableName:      src.TableName,
		ColumnName:     src.ColumnName,
		DataTypeName:   src.DataTypeName,
		ConstraintName: src.ConstraintName,
		driverErr:      src, // store original for Unwrap() and debugging
	}
}

// generateErrorCode creates consistent "application error codes" from DB errors.
//
// Output format:
//
//	<DOMAIN>_<ACTION>
//
// Example:
//
//	users + UniqueViolation => USER_ALREADY_EXISTS
//
// Rules:
//   - DOMAIN comes from tableName (uppercased, singularized crudely by removing trailing 'S')
//   - ACTION depends on violation type
//
// These codes are meant for machines (frontend logic, analytics), not humans.
func generateErrorCode(tableName string, errType Code) string {
	// If table is unknown, default to RECORD to avoid empty domain.
	if tableName == "" {
		tableName = "RECORD"
	}

	domain := strings.ToUpper(tableName)

	// Very naive singularization:
	// "USERS" -> "USER"
	// It won't handle "companies" etc, but good enough for many schemas.
	if strings.HasSuffix(domain, "S") && len(domain) > 1 {
		domain = domain[:len(domain)-1]
	}

	// Decide what kind of "action" code to generate.
	action := "ERROR"
	switch errType {
	case ForeignKeyViolation:
		action = "NOT_FOUND"
	case UniqueViolation:
		action = "ALREADY_EXISTS"
	case NotNullViolation:
		action = "REQUIRED"
	case CheckViolation:
		action = "INVALID"
	}

	return fmt.Sprintf("%s_%s", domain, action)
}

// formatUserFriendlyMessage produces an end-user-facing error message.
//
// This message is intended for clients / UI, not for logs.
// It uses table/column info to phrase messages in a more human way.
func formatUserFriendlyMessage(sqlErr *Error) string {
	// Pick an entity name that the message will refer to.
	entityName := getEntityName(sqlErr.TableName, sqlErr.ColumnName)

	switch sqlErr.Code {
	case ForeignKeyViolation:
		// Example: "The referenced user does not exist"
		return fmt.Sprintf("The referenced %s does not exist", entityName)

	case UniqueViolation:
		// Placeholder word "identifier" is later replaced if we can infer a column name.
		// Example becomes: "A user with this email already exists"
		return fmt.Sprintf("A %s with this identifier already exists", entityName)

	case NotNullViolation:
		// Use column name for "required" message.
		// Example: "The Email is required"
		fieldName := humanizeText(sqlErr.ColumnName)
		if fieldName == "" {
			fieldName = "field"
		}
		return fmt.Sprintf("The %s is required", fieldName)

	case CheckViolation:
		// CHECK constraints fail when values violate certain conditions.
		// Example: "The Age value does not meet required conditions"
		fieldName := humanizeText(sqlErr.ColumnName)
		if fieldName != "" {
			return fmt.Sprintf("The %s value does not meet required conditions", fieldName)
		}
		return "One or more values do not meet required conditions"

	default:
		// Fallback for unknown DB errors.
		return "An error occurred while processing your request"
	}
}

// getEntityName tries to infer an entity name from table/column data.
//
// Priority rules:
//  1. If column ends with "_id", use that base name. (Best for FK relations)
//     e.g. "user_id" -> "User"
//  2. Otherwise use table name, singularized if it ends with "s".
//  3. Otherwise fallback to "record".
func getEntityName(tableName, columnName string) string {
	// Most reliable for foreign keys: column like "user_id".
	if columnName != "" && strings.HasSuffix(strings.ToLower(columnName), "_id") {
		entity := strings.TrimSuffix(strings.ToLower(columnName), "_id")
		return humanizeText(entity)
	}

	// Fallback: table name.
	if tableName != "" {
		entity := tableName
		if strings.HasSuffix(entity, "s") && len(entity) > 1 {
			entity = entity[:len(entity)-1]
		}
		return humanizeText(entity)
	}

	return "record"
}

// humanizeText converts snake_case (or lower-ish identifiers) into Title Case.
//
// Example:
//
//	"first_name" -> "First Name"
//
// It uses x/text/cases for proper title casing rules.
func humanizeText(text string) string {
	if text == "" {
		return ""
	}
	return cases.Title(language.English).String(strings.ReplaceAll(text, "_", " "))
}

// extractColumnForUniqueViolation tries to infer the column name from a unique constraint name.
//
// It supports two conventions:
//
//  1. "unique_<table>_<column>"
//     Example: unique_users_email -> "email"
//
//  2. "<table>_<column>_(key|ukey)"
//     Example: users_email_key -> "email"
func extractColumnForUniqueViolation(constraintName string) string {
	if constraintName == "" {
		return ""
	}

	// Convention 1: unique_table_column
	if strings.HasPrefix(constraintName, "unique_") {
		parts := strings.Split(constraintName, "_")
		// Need at least: ["unique", "<table>", "<column>"]
		if len(parts) >= 3 {
			return parts[len(parts)-1]
		}
	}

	// Convention 2: table_column_key or table_column_ukey
	re := regexp.MustCompile(`_([^_]+)_(?:key|ukey)$`)
	matches := re.FindStringSubmatch(constraintName)
	if len(matches) > 1 {
		return matches[1]
	}

	return ""
}

// HandleError converts a low-level database error into an application-level error.
//
// Output:
//   - If already *errs.HTTPError: returned unchanged
//   - If pgconn.PgError: mapped into a specific errs.NewBadRequestError or errs.NewInternalServerError
//   - If ErrNoRows: mapped to errs.NewNotFoundError
//   - Otherwise: errs.NewInternalServerError
//
// This function is intended to be called in repositories/services after a DB call fails.
func HandleError(err error) error {
	// If it's already an HTTPError, don't re-wrap it.
	// This prevents double-wrapping and preserves exact error shape.
	var httpErr *errs.HTTPError
	if errors.As(err, &httpErr) {
		return err
	}

	// Handle Postgres server errors (constraint violations, etc.)
	//
	// pgconn.PgError is the primary Postgres error type from pgx.
	// It includes SQLSTATE and metadata.
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		// Convert into our structured error.
		sqlErr := ConvertPgError(pgerr)

		// Create:
		// - a machine-friendly error code (e.g. USER_ALREADY_EXISTS)
		// - a user-friendly message (e.g. "A user with this email already exists")
		errorCode := generateErrorCode(sqlErr.TableName, sqlErr.Code)
		userMessage := formatUserFriendlyMessage(sqlErr)

		switch sqlErr.Code {
		case ForeignKeyViolation:
			// Foreign key violation usually means reference doesn't exist.
			// Example: inserting post with user_id that doesn't exist.
			return errs.NewBadRequestError(userMessage, false, &errorCode, nil, nil)

		case UniqueViolation:
			// Unique violation means already exists.
			// Try to infer which column caused it and inject into message.
			columnName := extractColumnForUniqueViolation(sqlErr.ConstraintName)
			if columnName != "" {
				// Replace "identifier" placeholder with actual field name.
				userMessage = strings.ReplaceAll(userMessage, "identifier", humanizeText(columnName))
			}
			// override=true here suggests you want client UI to show this message directly.
			return errs.NewBadRequestError(userMessage, true, &errorCode, nil, nil)

		case NotNullViolation:
			// Not-null violation maps nicely to field-level errors for forms.
			fieldErrors := []errs.FieldError{
				{
					Field: strings.ToLower(sqlErr.ColumnName),
					Error: "is required",
				},
			}
			return errs.NewBadRequestError(userMessage, true, &errorCode, fieldErrors, nil)

		case CheckViolation:
			// CHECK constraint failures are also usually bad request.
			return errs.NewBadRequestError(userMessage, true, &errorCode, nil, nil)

		default:
			// Unknown/other DB errors should not leak details to clients.
			return errs.NewInternalServerError()
		}
	}

	// Handle "no rows found" errors (common for SELECT queries).
	// Both pgx and database/sql define ErrNoRows.
	switch {
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, sql.ErrNoRows):
		// The code tries to infer entity name from the error message.
		// This is brittle: error strings are not a stable API.
		errMsg := err.Error()
		tablePrefix := "table:"
		if strings.Contains(errMsg, tablePrefix) {
			// If error message includes "table:<name>:", extract the name.
			table := strings.Split(strings.Split(errMsg, tablePrefix)[1], ":")[0]
			entityName := getEntityName(table, "")
			return errs.NewNotFoundError(fmt.Sprintf("%s not found", entityName), true, nil)
		}
		// Generic not found fallback.
		return errs.NewNotFoundError("Resource not found", false, nil)
	}

	// Default fallback: treat unknown error as 500.
	return errs.NewInternalServerError()
}
