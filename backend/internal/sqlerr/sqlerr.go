// Package sqlerr specifically handles database driver errors.
//
// It parses cryptic error codes from the database driver and
// converts them into user-friendly messages (e.g., converting
// a "foreign key violation" into a "Bad Request" error)
package sqlerr
