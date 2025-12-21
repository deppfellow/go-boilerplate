// Package job provides background job processing using Asynq.
//
// Asynq is a Redis-backed job queue:
//   - You enqueue tasks (producer) using asynq.Client.
//   - A server runs workers that process those tasks (consumer) using asynq.Server.
package job

import (
	"github.com/deppfellow/go-boilerplate/internal/config"
	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
)

// JobService holds the Asynq client (enqueue) and server (worker execution).
type JobService struct {
	// Client is used to enqueue tasks into Redis.
	Client *asynq.Client

	// server runs worker processes that pull tasks from Redis and execute handlers.
	server *asynq.Server

	// logger is used for lifecycle logs and handler logs.
	logger *zerolog.Logger
}

// NewJobService creates a JobService configured to use Redis from cfg.
//
// It builds both:
//   - an asynq.Client (to push jobs)
//   - an asynq.Server (to process jobs)
//
// It also configures queue weights so "critical" tasks get more worker share.
func NewJobService(logger *zerolog.Logger, cfg *config.Config) *JobService {
	// Redis address where Asynq stores tasks, retries, schedules, etc.
	redisAddr := cfg.Redis.Address

	// Client for enqueuing tasks.
	client := asynq.NewClient(asynq.RedisClientOpt{
		Addr: redisAddr,
	})

	// Server for processing tasks.
	//
	// Concurrency = 10 means up to 10 tasks can be processed in parallel.
	// Queues weights distribute those workers across queues by ratio:
	//   critical: 6
	//   default:  3
	//   low:      1
	//
	// Roughly means: out of 10 tasks, ~6 can be critical, ~3 default, ~1 low.
	server := asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
		asynq.Config{
			Concurrency: 10,
			Queues: map[string]int{
				"critical": 6, // Higher priority queue for important emails
				"default":  3, // Default priority for most emails
				"low":      1, // Lower priority for non-urgent emails
			},
		},
	)

	return &JobService{
		Client: client,
		server: server,
		logger: logger,
	}
}

// Start starts the background worker server and registers task handlers.
//
// Flow:
//   - Create a ServeMux (routes task type -> handler function).
//   - Register handlers (TaskWelcome -> handleWelcomeEmailTask).
//   - Start the Asynq server (blocks until shutdown or error).
func (j *JobService) Start() error {
	// ServeMux is like HTTP routing, but for job types.
	mux := asynq.NewServeMux()

	// Register a handler for the "email:welcome" task type.
	mux.HandleFunc(TaskWelcome, j.handleWelcomeEmailTask)

	j.logger.Info().Msg("Starting background job server")

	// Start begins processing tasks. This typically blocks.
	// If it returns, it's usually due to shutdown or fatal error.
	if err := j.server.Start(mux); err != nil {
		return err
	}

	return nil
}

// Stop gracefully stops the job server and closes client resources.
//
// Shutdown stops workers and waits for current tasks to finish (depending on Asynq settings).
// Client.Close closes Redis connections used for enqueueing.
func (j *JobService) Stop() {
	j.logger.Info().Msg("Stopping background job server")
	j.server.Shutdown()
	j.Client.Close()
}
