package job

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/deppfellow/go-boilerplate/internal/config"
	"github.com/deppfellow/go-boilerplate/internal/lib/email"
	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
)

// emailClient is a package-level singleton used by job handlers.
//
// Caveat:
// This is global mutable state. If InitHandlers is not called before tasks run,
// handlers will panic on nil pointer when trying to use emailClient.
//
// A cleaner design is storing emailClient inside JobService as a field.
var emailClient *email.Client

// InitHandlers initializes dependencies required by job handlers.
//
// It constructs an email client using config + logger and stores it
// into the package-level emailClient variable.
func (j *JobService) InitHandlers(config *config.Config, logger *zerolog.Logger) {
	emailClient = email.NewClient(config, logger)
}

// handleWelcomeEmailTask processes the welcome email task.
//
// Steps:
//   - Parse JSON payload from the Asynq task
//   - Log task metadata
//   - Send the welcome email using emailClient
//   - Log success/failure
func (j *JobService) handleWelcomeEmailTask(ctx context.Context, t *asynq.Task) error {
	// Decode task payload (JSON bytes) into struct.
	var p WelcomeEmailPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("failed to unmarshal welcome email payload: %w", err)
	}

	// Log that we're processing the task, with some structured fields.
	j.logger.Info().
		Str("type", "welcome").
		Str("to", p.To).
		Msg("Processing welcome email task")

	// Perform the actual work: send the email.
	err := emailClient.SendWelcomeEmail(p.To, p.FirstName)
	if err != nil {
		// Log error with context.
		j.logger.Error().
			Str("type", "welcome").
			Str("to", p.To).
			Err(err).
			Msg("Failed to send welcome email")
		return err // returning err makes Asynq mark it failed and schedule retry
	}

	// Success log.
	j.logger.Info().
		Str("type", "welcome").
		Str("to", p.To).
		Msg("Successfully sent welcome email")

	return nil
}
