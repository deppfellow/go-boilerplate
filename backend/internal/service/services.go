// Package service contains the business logic.
//
// It sits between the handler and repository layers.
// It receives validated data from the handler, performs
// business operations, and calls repository methods to interact
// with the data
package service

import (
	"github.com/deppfellow/go-boilerplate/internal/lib/job"
	"github.com/deppfellow/go-boilerplate/internal/repository"
	"github.com/deppfellow/go-boilerplate/internal/server"
)

type Services struct {
	Auth *AuthService
	Job  *job.JobService
}

func NewService(s *server.Server, repos *repository.Repositories) (*Services, error) {
	authService := NewAuthService(s)

	return &Services{
		Job:  s.Job,
		Auth: authService,
	}, nil
}
