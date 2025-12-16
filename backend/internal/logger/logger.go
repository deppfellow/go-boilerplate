// Package logger configure the application's logging,
// monitoring, and observability.
//
// It uses *ZeroLog* for logging and integrates with
// *New Relic* to instrument the codebase, forwarding logs,
// metrics, and traces for debugging
package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/deppfellow/go-boilerplate/internal/config"
	"github.com/newrelic/go-agent/v3/integrations/logcontext-v2/zerologWriter"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

// LoggerService is a small wrapper around a New Relic application.
// It exists so other packages can ask "do we have New Relic enabled?".
type LoggerService struct {
	nrApp *newrelic.Application
}

// NewLoggerService initializes New Relic based on observability config.
//
// If no license key is provided, it skips New Relic initialization and returns
// a service with nrApp nil.
//
// Note: This prints to stdout using fmt.Println instead of structured logging.
// That’s acceptable during early startup, but inconsistent.
func NewLoggerService(cfg *config.ObservabilityConfig) *LoggerService {
	service := &LoggerService{}

	// If license key isn't provided, treat New Relic as disabled.
	if cfg.NewRelic.LicenseKey == "" {
		fmt.Println("New relic license key is not provided, skipping initialization")
		return service
	}

	// Build New Relic config options.
	// Each ConfigOption modifies the config for the New Relic agent.
	var configOptions []newrelic.ConfigOption
	configOptions = append(configOptions,
		newrelic.ConfigAppName(cfg.ServiceName),
		newrelic.ConfigLicense(cfg.NewRelic.LicenseKey),
		newrelic.ConfigAppLogForwardingEnabled(cfg.NewRelic.AppLogForwardingEnabled),
		newrelic.ConfigDistributedTracerEnabled(cfg.NewRelic.DistributedTracingEnabled),
	)

	// Enable debug logging only if explicitly enabled.
	// Debug logger writes to stdout which can be noisy.
	if cfg.NewRelic.DebugLogging {
		configOptions = append(configOptions, newrelic.ConfigDebugLogger(os.Stdout))
	}

	// Create the New Relic application instance.
	// This starts the agent and may connect/initialize internal state.
	app, err := newrelic.NewApplication(configOptions...)
	if err != nil {
		// Note: error is swallowed and service returned with nrApp nil.
		// A production setup usually logs the error.
		return service
	}

	service.nrApp = app
	return service

}

// Shutdown shuts down New Relic gracefully.
//
// It waits up to 10 seconds for pending harvest/export operations.
func (ls *LoggerService) Shutdown() {
	if ls.nrApp != nil {
		ls.nrApp.Shutdown(10 * time.Second)
	}
}

// GetApplication returns the New Relic application instance (or nil if disabled).
func (ls *LoggerService) GetApplication() *newrelic.Application {
	return ls.nrApp
}

// NewLogger is a convenience wrapper that creates a logger with just a level and prod flag.
//
// It constructs a minimal ObservabilityConfig and passes it to NewLoggerWithService.
// NOTE: ServiceName and Logging.Format are not set here, so they may end up empty unless
// GetLogLevel / IsProduction logic covers your needs.
func NewLogger(level string, isProd bool) zerolog.Logger {
	return NewLoggerWithService(&config.ObservabilityConfig{
		Logging: config.LoggingConfig{
			Level: level,
		},
		Environment: func() string {
			if isProd {
				return "production"
			}
			return "development"
		}(),
	}, nil)
}

// NewLoggerWithConfig creates a logger with full config (backward compatibility).
// It just calls NewLoggerWithService with a nil LoggerService.
func NewLoggerWithConfig(cfg *config.ObservabilityConfig) zerolog.Logger {
	return NewLoggerWithService(cfg, nil)
}

// NewLoggerWithService creates a configured zerolog logger.
//
// It selects:
//   - effective log level (based on cfg.GetLogLevel())
//   - output format: JSON for production if configured, otherwise console
//   - New Relic log forwarding wrapper in production when enabled
//
// It also attaches default fields:
//   - service
//   - environment
func NewLoggerWithService(cfg *config.ObservabilityConfig, loggerService *LoggerService) zerolog.Logger {
	// Convert string level ("debug"/"info"/...) into zerolog.Level.
	var logLevel zerolog.Level
	level := cfg.GetLogLevel()

	switch level {
	case "debug":
		logLevel = zerolog.DebugLevel
	case "info":
		logLevel = zerolog.InfoLevel
	case "warn":
		logLevel = zerolog.WarnLevel
	case "error":
		logLevel = zerolog.ErrorLevel
	default:
		logLevel = zerolog.InfoLevel
	}

	// Global zerolog settings.
	// TimeFieldFormat sets the timestamp format for log entries.
	// Don't set global level - let each logger have its own level
	zerolog.TimeFieldFormat = "2006-01-02 15:04:05"

	// ErrorStackMarshaler tells zerolog how to encode stack traces.
	// pkgerrors.MarshalStack supports github.com/pkg/errors stack frames.
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	var writer io.Writer

	// Setup writer:
	// - Production + json: write structured logs to stdout (good for log ingestion)
	// - Otherwise: pretty console output for development readability
	var baseWriter io.Writer
	if cfg.IsProduction() && cfg.Logging.Format == "json" {
		// In production, write to stdout
		baseWriter = os.Stdout

		// Wrap stdout writer with New Relic zerologWriter integration if enabled.
		// This allows New Relic log forwarding while still writing to stdout.
		if loggerService != nil && loggerService.nrApp != nil {
			nrWriter := zerologWriter.New(baseWriter, loggerService.nrApp)
			writer = nrWriter
		} else {
			writer = baseWriter
		}
	} else {
		// Development mode - use console writer
		// Development (or non-json format) uses ConsoleWriter for readable logs.
		consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "2006-01-02 15:04:05"}
		writer = consoleWriter
	}

	// Note: New Relic log forwarding is now handled automatically by zerologWriter integration

	// Build the logger with:
	// - output writer
	// - level filter
	// - default fields (timestamp + service + environment)
	logger := zerolog.New(writer).
		Level(logLevel).
		With().
		Timestamp().
		Str("service", cfg.ServiceName).
		Str("environment", cfg.Environment).
		Logger()

	// Include stack traces for errors in development
	// Add stack traces for errors in development for easier debugging.
	// In production, stack traces often create noise or leak internals.
	if !cfg.IsProduction() {
		logger = logger.With().Stack().Logger()
	}

	return logger
}

// WithTraceContext adds trace/span IDs from a New Relic transaction into the logger.
//
// This is used to correlate logs with traces.
// If txn is nil, it returns the original logger unchanged.
func WithTraceContext(logger zerolog.Logger, txn *newrelic.Transaction) zerolog.Logger {
	if txn == nil {
		return logger
	}

	// Pull trace metadata from New Relic transaction.
	metadata := txn.GetTraceMetadata()

	// Attach trace.id and span.id as structured fields.
	return logger.With().
		Str("trace.id", metadata.TraceID).
		Str("span.id", metadata.SpanID).
		Logger()
}

// NewPgxLogger creates a database-focused logger used for pgx query tracing.
//
// It uses ConsoleWriter and custom formatting for field values to improve readability:
//   - long SQL strings are truncated
//   - JSON blobs in []byte are pretty-printed
func NewPgxLogger(level zerolog.Level) zerolog.Logger {
	writer := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: "2006-01-02 15:04:05",

		// FormatFieldValue customizes how field values appear in console output.
		// This affects how pgx tracelog entries look.
		FormatFieldValue: func(i any) string {
			switch v := i.(type) {
			case string:
				// Clean and format SQL for better readability
				if len(v) > 200 {
					// Truncate very long SQL statements
					return v[:200] + "..."
				}
				return v
			case []byte:
				// Try to pretty-print JSON if the value is JSON bytes.
				var obj interface{}
				if err := json.Unmarshal(v, &obj); err == nil {
					pretty, _ := json.MarshalIndent(obj, "", "    ")
					return "\n" + string(pretty)
				}

				// Otherwise treat as raw bytes -> string.
				return string(v)
			default:
				// Fallback formatting for other types.
				return fmt.Sprintf("%v", v)
			}
		},
	}

	return zerolog.New(writer).
		Level(level).
		With().
		Timestamp().
		Str("component", "database").
		Logger()
}

// GetPgxTraceLogLevel converts a zerolog level to a pgx tracelog level.
//
// pgx tracelog levels are numeric constants. This function maps them:
//   - debug -> 6 (TraceLog debug)
//   - info  -> 4
//   - warn  -> 3
//   - error -> 2
//
// default -> 0 (none)
//
// Note: This assumes the numeric values match pgx tracelog constants.
func GetPgxTraceLogLevel(level zerolog.Level) int {
	switch level {
	case zerolog.DebugLevel:
		return 6 // tracelog.LogLevelDebug
	case zerolog.InfoLevel:
		return 4 // tracelog.LogLevelInfo
	case zerolog.WarnLevel:
		return 3 // tracelog.LogLevelWarn
	case zerolog.ErrorLevel:
		return 2 // tracelog.LogLevelError
	default:
		return 0 // tracelog.LogLevelNone
	}
}
