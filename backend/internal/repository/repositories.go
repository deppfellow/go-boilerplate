// Package repository handles all interactions with the database.
//
// It contains raw SQL queries and methods to fetch, persist,
// or update data, abstracting SQL logic away from the service layer.
//
// Tutor notes (02:49:23–02:50:35):
//   - Create a Repositories struct that will embed all repository instances.
//   - Even if the boilerplate has no repos yet, keep an empty container to satisfy
//     service dependencies and preserve the DI structure.
//   - Later, as you add features, you add fields like UsersRepo, TodosRepo, etc.
package repository

import (
	"github.com/deppfellow/go-boilerplate/internal/server"
)

// Repositories is a container for all repository instances.
//
// In a real app, this typically holds repo structs that perform database operations, e.g.:
//
//	type Repositories struct {
//	    Users *UsersRepository
//	    Todos *TodosRepository
//	}
//
// In this boilerplate stage it’s intentionally empty:
// - to establish the dependency injection shape early
// - so Services can accept repos even before concrete repositories exist
type Repositories struct{}

// NewRepositories constructs the repository container.
//
// Parameter:
// - s: application container (DB pool lives on s.DB, logger on s.Logger, etc.)
//
// In this minimal boilerplate the returned container is empty, but the constructor exists so
// future repositories can be initialized here using s.DB.Pool and other shared deps.
func NewRepositories(s *server.Server) *Repositories {
	_ = s // intentionally unused for now; reserved for future repo initialization
	return &Repositories{}
}
