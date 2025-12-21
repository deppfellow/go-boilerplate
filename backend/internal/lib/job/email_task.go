package job

import (
	"encoding/json"
	"time"

	"github.com/hibiken/asynq"
)

const (
	// TaskWelcome is the job type name stored in Redis.
	// Asynq uses task type strings to route to handlers.
	TaskWelcome = "email:welcome"
)

// WelcomeEmailPayload is the JSON payload data for the welcome email task.
//
// This gets serialized into bytes and stored in Redis.
type WelcomeEmailPayload struct {
	To        string `json:"to"`
	FirstName string `json:"first_name"`
}

// NewWelcomeEmailTask constructs an Asynq task for sending a welcome email.
//
// It serializes payload to JSON and configures task options:
//   - MaxRetry(3): retry up to 3 times on failure
//   - Queue("default"): send into the "default" queue
//   - Timeout(30s): kill the task if handler runs longer than 30 seconds
func NewWelcomeEmailTask(to, firstName string) (*asynq.Task, error) {
	// Serialize payload into JSON bytes.
	payload, err := json.Marshal(WelcomeEmailPayload{
		To:        to,
		FirstName: firstName,
	})
	if err != nil {
		return nil, err
	}

	// Create task:
	//   type = TaskWelcome ("email:welcome")
	//   payload = JSON bytes
	//   options = retry/queue/timeout behavior
	return asynq.NewTask(
		TaskWelcome,
		payload,
		asynq.MaxRetry(3),
		asynq.Queue("default"),
		asynq.Timeout(30*time.Second),
	), nil
}
