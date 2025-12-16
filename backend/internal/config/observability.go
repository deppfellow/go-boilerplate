package config

import (
	"fmt"
	"time"
)

// ObservabilityConfig groups all configuration related to telemetry and runtime visibility.
//
// This typically includes:
//   - logging settings (format, level, thresholds)
//   - APM/tracing provider settings (New Relic here)
//   - health check settings (liveness/readiness style checks)
//
// It is intended to be embedded under Config.Observability and can be optional
// at the root-level (pointer in Config). If omitted, defaults are injected.
type ObservabilityConfig struct {
	// ServiceName identifies this service in logs/traces/APM dashboards.
	// Usually hardcoded per service to avoid people "configuring" it into chaos.
	ServiceName string `koanf:"service_name" validate:"required"`

	// Environment is a label used to split telemetry by environment
	// (production, staging, development, etc.).
	Environment string `koanf:"environment" validate:"required"`

	// Logging config controls structured logger behavior.
	Logging LoggingConfig `koanf:"logging" validate:"required"`

	// NewRelic config controls APM and tracing features.
	NewRelic NewRelicConfig `koanf:"new_relic" validate:"required"`

	// HealthChecks config controls periodic dependency health checks.
	HealthChecks HealthChecksConfig `koanf:"health_checks" validate:"required"`
}

// LoggingConfig holds application logging configuration.
type LoggingConfig struct {
	// Level is the verbosity threshold (debug/info/warn/error).
	// Any logs below this level are ignored.
	Level string `koanf:"level" validate:"required"`

	// Format selects the output format for logs (commonly "json" or "console").
	// This codebase defaults to JSON, probably so log pipelines don’t cry.
	Format string `koanf:"format" validate:"required"`

	// SlowQueryThreshold is a duration beyond which queries are considered slow
	// and should be logged/flagged. Optional, defaults can be set.
	//
	// Type is time.Duration, so env/config should supply parseable duration
	// strings like "100ms", "1s", "250ms". If you supply "100" it will not mean
	// 100ms; it will mean 100ns if parsed incorrectly elsewhere.
	SlowQueryThreshold time.Duration `koanf:"slow_query_threshold"`
}

// NewRelicConfig holds configuration for New Relic APM and tracing.
//
// LicenseKey is required if New Relic is actually used. Others are feature toggles.
type NewRelicConfig struct {
	// LicenseKey is the New Relic ingest key. Empty means "not configured".
	LicenseKey string `koanf:"license_key" validate:"required"`

	// AppLogForwardingEnabled enables forwarding of application logs to New Relic
	// (if the agent supports it and is configured).
	AppLogForwardingEnabled bool `koanf:"app_log_forwarding_enabled"`

	// DistributedTracingEnabled enables distributed tracing so requests can be traced
	// across service boundaries.
	DistributedTracingEnabled bool `koanf:"distributed_tracing_enabled"`

	// DebugLogging enables debug output for the agent/integration.
	// Usually off in production to avoid noisy logs and format pollution.
	DebugLogging bool `koanf:"debug_logging"`
}

// HealthChecksConfig controls periodic checks for dependencies.
//
// This is typically used for:
//   - internal monitoring
//   - liveness/readiness endpoints (if implemented)
//   - proactive alerting when dependencies degrade
type HealthChecksConfig struct {
	// Enabled toggles health checking logic entirely.
	Enabled bool `koanf:"enabled"`

	// Interval is how frequently checks run.
	// validate:"min=1s" means validator expects >= 1 second.
	Interval time.Duration `koanf:"interval" validate:"min=1s"`

	// Timeout is the max time allowed for a check run before it is considered failed.
	Timeout time.Duration `koanf:"timeout" validate:"min=1s"`

	// Checks is a list of check names to run (e.g. database, redis).
	// The code elsewhere likely maps these strings to actual check functions.
	Checks []string `koanf:"checks"`
}

// DefaultObservabilityConfig provides a safe set of defaults.
//
// Used when Config.Observability is nil (not provided via env/config).
// Defaults aim to be sensible for local dev, while not breaking production.
func DefaultObservabilityConfig() *ObservabilityConfig {
	return &ObservabilityConfig{
		// Default service/environment are overwritten in loadConfig()
		// in your config.go: ServiceName forced to "boilerplate",
		// Environment derived from primary.env.
		ServiceName: "boilerplate",
		Environment: "development",

		// Logging defaults:
		// - info level avoids debug spam
		// - json format works well in log aggregators
		// - 100ms threshold is a common "hmm maybe slow" boundary
		Logging: LoggingConfig{
			Level:              "info",
			Format:             "json",
			SlowQueryThreshold: 100 * time.Millisecond,
		},

		// New Relic defaults:
		// - LicenseKey empty (but note: validate tags + Validate() can conflict with this)
		// - app log forwarding + distributed tracing enabled by default
		// - debug off to prevent mixed log formats/noise
		NewRelic: NewRelicConfig{
			LicenseKey:                "",
			AppLogForwardingEnabled:   true,
			DistributedTracingEnabled: true,
			DebugLogging:              false, // Disabled by default to avoid mixed log formats
		},

		// Health checks defaults:
		// - enabled
		// - check every 30 seconds, allow 5 seconds per run
		// - default checks include database + redis
		HealthChecks: HealthChecksConfig{
			Enabled:  true,
			Interval: 30 * time.Second,
			Timeout:  5 * time.Second,
			Checks:   []string{"database", "redis"},
		},
	}
}

// Validate applies custom validation rules that go beyond struct tags.
//
// This is *separate* from go-playground/validator tags used in config.go.
// It's useful for validating enums, cross-field constraints, and business rules.
//
// Returns:
//   - nil if configuration is valid
//   - an error describing the first validation failure
func (c *ObservabilityConfig) Validate() error {
	// ServiceName must not be empty. This is partially redundant with validate:"required",
	// but needed if you ever bypass the struct-tag validator or set values manually.
	if c.ServiceName == "" {
		return fmt.Errorf("service_name is required")
	}

	// Validate log levels: enforce a strict set of allowed values.
	// Using a map provides O(1) lookup for membership.
	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}

	// If the configured level isn't in the map, reject it.
	// This prevents typos like "inf" silently degrading into nonsense.
	if !validLevels[c.Logging.Level] {
		return fmt.Errorf("invalid logging level: %s (must be one of: debug, info, warn, error)", c.Logging.Level)
	}

	// Validate slow query threshold:
	// duration < 0 makes no sense (you can’t be slower than negative time).
	if c.Logging.SlowQueryThreshold < 0 {
		return fmt.Errorf("logging slow_query_threshold mus be non-negative")
	}

	return nil
}

// GetLogLevel returns the effective log level to use at runtime.
//
// It supports "defaulting by environment":
//   - In production: default to "info" if no level is set.
//   - In development: default to "debug" if no level is set.
//
// Otherwise it returns whatever c.Logging.Level is set to.
func (c *ObservabilityConfig) GetLogLevel() string {
	switch c.Environment {
	case "production":
		// Production defaults to info if nothing is set.
		if c.Logging.Level == "" {
			return "info"
		}
	case "development":
		// Development defaults to debug if nothing is set.
		if c.Logging.Level == "" {
			return "debug"
		}
	}

	// If environment is neither "production" nor "development",
	// or level is explicitly set, just return the configured value.
	return c.Logging.Level
}

// IsProduction reports whether the application is running in production mode.
//
// This is typically used to:
//   - enable/disable debug features
//   - change log verbosity
//   - tighten security defaults
func (c *ObservabilityConfig) IsProduction() bool {
	return c.Environment == "production"
}
