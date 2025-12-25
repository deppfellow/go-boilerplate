// Package repository handles all interactions with the database.
//
// It contains raw SQL queries and methods to fetch, persist,
// or update data, abstracting SQL logic away from the service layer
package repository

import (
	"github.com/deppfellow/go-boilerplate/internal/server"
)

type Repositories struct{}

func NewRepositories(s *server.Server) *Repositories {
	return &Repositories{}
}
