// Package server defines the core Server struct that composes the app's main dependencies.
//
// It contains the initialization logic to spin up the HTTP server
// and handles graceful shutdowns
//
// It owns the lifecycle of:
//   - configuration
//   - logger + optional New Relic service wrapper
//   - database pool
//   - redis client
//   - background job worker server (asynq)
//   - http.Server
//
// It provides constructors and start/shutdown logic to run the application cleanly.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/deppfellow/go-boilerplate/internal/config"
	"github.com/deppfellow/go-boilerplate/internal/database"
	"github.com/deppfellow/go-boilerplate/internal/lib/job"
	"github.com/newrelic/go-agent/v3/integrations/nrredis-v9"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	loggerPkg "github.com/deppfellow/go-boilerplate/internal/logger"
)

// Server is the application container that holds shared resources.
//
// It is not the HTTP server itself. It holds:
//   - the config
//   - the logger(s)
//   - database and redis connections
//   - background job service
//   - an internal *http.Server used to listen and serve requests
type Server struct {
	// Config holds all environment/config values for the app.
	Config *config.Config

	// Logger is the application's main structured logger.
	Logger *zerolog.Logger

	// LoggerService optionally holds the New Relic application instance.
	// If New Relic is disabled, this may exist but contain nil nrApp.
	LoggerService *loggerPkg.LoggerService

	// DB holds the PostgreSQL pool wrapper.
	DB *database.Database

	// Redis is the Redis client.
	Redis *redis.Client

	// httpServer is the standard library HTTP server instance.
	// It is configured in SetupHTTPServer and started in Start().
	httpServer *http.Server

	// Job runs background workers (Asynq server) and provides a client for enqueueing.
	Job *job.JobService
}

// New constructs a Server and initializes core dependencies.
//
// It does NOT start the HTTP server directly. That is done in SetupHTTPServer + Start.
//
// Initialization performed:
//   - PostgreSQL pool + optional New Relic tracing
//   - Redis client + optional New Relic hooks
//   - JobService (Asynq client/server) + start job worker
//
// Notes:
//   - Redis connection failure does not block startup (it logs and continues).
//   - JobService Start failure DOES block startup (returns error).
func New(cfg *config.Config, logger *zerolog.Logger, loggerService *loggerPkg.LoggerService) (*Server, error) {
	// Initialize PostgreSQL pool.
	// This also pings the DB to ensure connectivity.
	db, err := database.New(cfg, logger, loggerService)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Create a Redis client.
	// This does not actually connect immediately; Redis connections are lazy.
	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.Redis.Address,
	})

	// Add New Relic Redis hooks if New Relic is enabled.
	//
	// Hooks instrument Redis operations (commands timing, errors, etc.)
	// so they show up in distributed traces.
	if loggerService != nil && loggerService.GetApplication() != nil {
		redisClient.AddHook(nrredis.NewHook(redisClient.Options()))
	}

	// Test Redis connection with a timeout so it doesn't hang startup.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Ping sends a PING command to Redis.
	// If it fails, we log but do not stop startup.
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Error().Err(err).Msg("Failed to connect to Redis, continuing without Redis")
		// By not returning error, you are choosing "Redis optional".
		// This is sometimes OK, but dangerous if core features require Redis.
	}

	// Create background job service (Asynq).
	// It uses Redis internally as its backing store.
	jobService := job.NewJobService(logger, cfg)

	// Initialize job handlers (sets up email client, etc.).
	// Important: as written, handlers rely on global emailClient in the job package.
	jobService.InitHandlers(cfg, logger)

	// Start job server.
	//
	// Important behavior:
	// asynq.Server.Start(...) typically BLOCKS until shutdown.
	// If that's true here, this code will never proceed to return the Server.
	//
	// Some people run Start() in a goroutine. If your version of Asynq blocks,
	// you'll need to wrap it.
	if err := jobService.Start(); err != nil {
		return nil, err
	}

	// Construct the Server container.
	server := &Server{
		Config:        cfg,
		Logger:        logger,
		LoggerService: loggerService,
		DB:            db,
		Redis:         redisClient,
		Job:           jobService,
	}

	// Runtime metrics comment:
	// New Relic Go agent may collect runtime metrics automatically if enabled.

	return server, nil
}

// SetupHTTPServer configures the internal net/http server.
//
// The actual router/mux is passed in as handler.
// Echo can provide a net/http handler via e.StartServer / e.Server, etc.
func (s *Server) SetupHTTPServer(handler http.Handler) {
	s.httpServer = &http.Server{
		// Bind to port from config.
		Addr: ":" + s.Config.Server.Port,

		// Handler is your router/middleware stack.
		Handler: handler,

		// These timeouts protect against slow clients and resource exhaustion.
		// Config stores int values, interpreted here as seconds.
		ReadTimeout:  time.Duration(s.Config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.Config.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(s.Config.Server.IdleTimeout) * time.Second,
	}
}

// Start runs the HTTP server.
//
// It requires SetupHTTPServer to be called first.
func (s *Server) Start() error {
	// Guard clause: without httpServer configured, Start can't run.
	if s.httpServer == nil {
		// Typo in original: "nit" -> "not"
		return errors.New("HTTP server nit initialized")
	}

	// Log startup info.
	s.Logger.Info().
		Str("port", s.Config.Server.Port).
		Str("env", s.Config.Primary.Env).
		Msg("starting server")

	// ListenAndServe starts accepting requests.
	// It blocks until the server stops or errors.
	//
	// If you want graceful shutdown, you call s.Shutdown(ctx) from a signal handler.
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server and its dependencies.
//
// It attempts to:
//   - stop HTTP server (finish inflight requests until ctx deadline)
//   - close DB pool
//   - stop job service (asynq) if it exists
//
// Note: Redis client is NOT closed here, which is usually fine but not ideal.
func (s *Server) Shutdown(ctx context.Context) error {
	// Gracefully stop the HTTP server.
	// It stops accepting new connections and waits for ongoing requests.
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown HTTP server: %w", err)
	}

	// Close database connection pool.
	if err := s.DB.Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}

	// Stop background jobs.
	if s.Job != nil {
		s.Job.Stop()
	}

	return nil
}
