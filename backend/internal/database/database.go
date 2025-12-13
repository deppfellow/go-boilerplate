// Package database contains the logic for establishing
// connections to the PostgreSQL database.
//
// It specifically handles *database pooling* (maintaining..
// active connections for efficiency) and integrating..
// the logger/tracer with the database driver (PGX).
package database
