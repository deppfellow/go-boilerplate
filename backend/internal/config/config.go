// Package config manages environment variables.
//
// It reads variable from the `.env` file,
// loads them into structured Go types (struct), and
// validates that required values are present so they
// can be reused accross the application runtime.
package config
