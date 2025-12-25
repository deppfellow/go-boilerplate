// Package service contains the business logic.
//
// It sits between the handler and repository layers.
// It receives validated data from the handler, performs
// business operations, and calls repository methods to interact
// with the data.
//
// Tutor notes (02:48:06–02:50:35):
//   - Create a single "Services" struct that embeds all service instances.
//   - Even if the boilerplate only has one real service today (Auth/Clerk),
//     this pattern scales when you add more services later (TodoService, UserService, etc.).
//   - Services are initialized with dependencies (server + repositories) to keep
//     the dependency injection workflow consistent.
package service

import (
	"github.com/deppfellow/go-boilerplate/internal/lib/job"
	"github.com/deppfellow/go-boilerplate/internal/repository"
	"github.com/deppfellow/go-boilerplate/internal/server"
)

// Services is a container that groups all business services.
//
// Why a container struct?
// - Keeps initialization in one place.
// - Makes it easy to pass dependencies into services (server resources, repositories).
// - Makes it easy for handlers to depend on a single object rather than many.
//
// Current boilerplate services:
// - Auth: initializes Clerk with the secret key from config.
// - Job: background job service (Asynq) already created earlier and attached to Server.
type Services struct {
	Auth *AuthService
	Job  *job.JobService
}

// NewService constructs and wires the service layer.
//
// Parameters:
// - s: the application container (config, logger, db, redis, job, etc.)
// - repos: repository container (even if empty now, it standardizes DI)
//
// Returns:
// - *Services: the initialized service container
// - error: reserved for future expansion when service initialization can fail
//
// Tutor reasoning:
//   - Pass repositories as a dependency so each service can receive only the repos it needs.
//     Example: a future TodoService could receive repos.Todos.
func NewService(s *server.Server, repos *repository.Repositories) (*Services, error) {
	// Initialize Auth service (Clerk setup happens inside).
	authService := NewAuthService(s)

	// Job service is already created and started inside server.New(...),
	// so we reuse the instance from Server here.
	return &Services{
		Job:  s.Job,
		Auth: authService,
	}, nil
}
