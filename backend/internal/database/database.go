// Package database contains the logic for establishing
// connections to the PostgreSQL database.
//
// It specifically handles *database pooling* (maintaining..
// active connections for efficiency) and integrating..
// the logger/tracer with the database driver (PGX).
//
// It handles:
//   - building a DSN from config
//   - creating a pgx connection pool (pgxpool)
//   - wiring query tracing/logging (pgx tracelog)
//   - optional New Relic instrumentation (nrpgx5)
package database

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/deppfellow/go-boilerplate/internal/config"
	loggerConfig "github.com/deppfellow/go-boilerplate/internal/logger"
	pgxzero "github.com/jackc/pgx-zerolog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/newrelic/go-agent/v3/integrations/nrpgx5"
	"github.com/rs/zerolog"
)

// Database wraps the pgx connection pool and a logger.
// It provides a simple object you can pass around the app.
//
// Pool is the shared connection pool.
// log is used for lifecycle logs (connect/close, etc.).
type Database struct {
	Pool *pgxpool.Pool
	log  *zerolog.Logger
}

// multiTracer allows chaining multiple tracers.
//
// pgx supports a single Tracer in ConnConfig.
// This type acts as an adapter so you can run multiple tracer implementations:
//   - New Relic tracer (for distributed tracing/APM)
//   - tracelog.TraceLog (for local SQL logging in "local" env)
//
// Implementation detail:
//   - Uses runtime interface checks to see whether each tracer supports
//     TraceQueryStart and TraceQueryEnd.
type multiTracer struct {
	tracers []any
}

// TraceQueryStart implements pgx tracer interface.
//
// Called at the start of query execution.
// Returning a context allows tracers to store values for TraceQueryEnd later.
//
// This method loops through all tracers and if they implement TraceQueryStart,
// it calls them in order, threading the context through each call.
func (mt *multiTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	for _, tracer := range mt.tracers {
		// Runtime type assertion: does this tracer implement TraceQueryStart?
		if t, ok := tracer.(interface {
			TraceQueryStart(context.Context, *pgx.Conn, pgx.TraceQueryStartData) context.Context
		}); ok {
			// Some tracers may attach metadata to ctx; we keep it.
			ctx = t.TraceQueryStart(ctx, conn, data)
		}
	}
	return ctx
}

// TraceQueryEnd implements pgx tracer interface.
//
// Called after query execution completes.
// This function loops through tracers and calls TraceQueryEnd where supported.
func (mt *multiTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	for _, tracer := range mt.tracers {
		if t, ok := tracer.(interface {
			TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData)
		}); ok {
			t.TraceQueryEnd(ctx, conn, data)
		}
	}
}

// DatabasePingTimeout defines the number of seconds to wait for a ping
// before considering the database "unreachable".
//
// Note: it's an int, used as DatabasePingTimeout * time.Second below.
const DatabasePingTimeout = 10

// New creates a PostgreSQL connection pool with instrumentation.
//
// Inputs:
//   - cfg: application config (host, port, user, password, pool settings, etc.)
//   - logger: main app logger
//   - loggerService: optional New Relic service (nil if not configured)
//
// Behavior:
//   - Build DSN safely (URL-escape password)
//   - Parse DSN into pgxpool config
//   - Attach New Relic tracer if available
//   - In local env: attach SQL tracelogger (and chain tracers if both exist)
//   - Create pool, ping it, and return Database
func New(cfg *config.Config, logger *zerolog.Logger, loggerService *loggerConfig.LoggerService) (*Database, error) {
	// Joins host + port safely.
	// Handles IPv6 correctly (adds brackets if needed).
	hostPort := net.JoinHostPort(cfg.Database.Host, strconv.Itoa(cfg.Database.Port))

	// URL-encode the password to prevent breaking the DSN.
	// Example: "pa:ss@word" would otherwise destroy the URL structure.
	encodedPassword := url.QueryEscape(cfg.Database.Password)

	// DSN (connection string) using the postgres URL scheme.
	// Example:
	//   postgres://user:pass@localhost:5432/dbname?sslmode=disable
	dsn := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=%s",
		cfg.Database.User,
		encodedPassword,
		hostPort,
		cfg.Database.Name,
		cfg.Database.SSLMode,
	)

	// Parse the DSN into a pgxpool config structure.
	// This also applies pgx defaults and validates format.
	pgxPoolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pgx pool config: %w", err)
	}

	// Add New Relic PostgreSQL instrumentation.
	// This sets pgxPoolConfig.ConnConfig.Tracer (single tracer slot).
	//
	// Only enabled if loggerService exists and has an app instance.
	if loggerService != nil && loggerService.GetApplication() != nil {
		pgxPoolConfig.ConnConfig.Tracer = nrpgx5.NewTracer()
	}

	// In local env, enable SQL query logging using pgx tracelog + zerolog.
	//
	// This is very noisy, which is why it’s only in local.
	if cfg.Primary.Env == "local" {
		// Get the current configured log level from the app logger.
		globalLevel := logger.GetLevel()

		// Create a specialized logger for pgx output (pretty printing SQL/params).
		pgxLogger := loggerConfig.NewPgxLogger(globalLevel)

		// If a tracer already exists (e.g. New Relic), chain both using multiTracer.
		if pgxPoolConfig.ConnConfig.Tracer != nil {
			// localTracer is pgx's built-in tracer that logs queries.
			localTracer := &tracelog.TraceLog{
				// pgxzero adapts zerolog to pgx tracelog.Logger interface.
				Logger: pgxzero.NewLogger(pgxLogger),

				// Convert zerolog level to pgx tracelog level.
				LogLevel: tracelog.LogLevel(loggerConfig.GetPgxTraceLogLevel(globalLevel)),
			}

			// multiTracer ensures both tracers run:
			// 1) existing tracer (New Relic)
			// 2) local tracer (SQL log output)
			pgxPoolConfig.ConnConfig.Tracer = &multiTracer{
				tracers: []any{pgxPoolConfig.ConnConfig.Tracer, localTracer},
			}
		} else {
			// If no tracer exists, use only the local SQL tracer.
			pgxPoolConfig.ConnConfig.Tracer = &tracelog.TraceLog{
				Logger:   pgxzero.NewLogger(pgxLogger),
				LogLevel: tracelog.LogLevel(loggerConfig.GetPgxTraceLogLevel(globalLevel)),
			}
		}
	}

	// Create the connection pool with the prepared config.
	// context.Background is OK at init time since pool creation is fast,
	// but you could also use a startup context.
	pool, err := pgxpool.NewWithConfig(context.Background(), pgxPoolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pgx pool: %w", err)
	}

	// Wrap pool + logger in Database struct for easier wiring.
	database := &Database{
		Pool: pool,
		log:  logger,
	}

	// Ping the DB with a timeout, so startup fails fast if DB is down.
	ctx, cancel := context.WithTimeout(context.Background(), DatabasePingTimeout*time.Second)
	defer cancel()
	if err = pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Info().Msg("connected to the database")

	return database, nil
}

// Close closes the database connection pool.
//
// pgxpool.Pool.Close() is idempotent-ish and frees resources.
// Returns nil currently because pgxpool.Close doesn't return error.
func (db *Database) Close() error {
	db.log.Info().Msg("closing database connection pool")
	db.Pool.Close()
	return nil
}
