// Package handler is the first layer. The first entry point
// for business logic after the router.
//
// It parses requests, handles input validation using the..
// validation package, and calls the appropriate service layer.
// It acts as the interface between the HTTP request and the core..
// business logic.
package handler

import (
	"github.com/deppfellow/go-boilerplate/internal/server"
	"github.com/deppfellow/go-boilerplate/internal/service"
)

// Handlers is a container that groups all HTTP handlers.
//
// Tutor notes (02:50:35–02:51:55):
//   - Similar to Middlewares and Services, create a single Handlers struct.
//   - This keeps router setup clean: you pass one object around instead of many.
//   - Handlers represent the HTTP layer: parse input, validate, call services,
//     and return responses.
type Handlers struct {
	Health  *HealthHandler  // Health serves service health endpoints (liveness/readiness).
	OpenAPI *OpenAPIHandler // OpenAPI serves API documentation (OpenAPI spec / swagger endpoints).
}

// NewHandlers constructs the handler container.
//
// Parameters:
// - s: application container (logger/config/etc.) often needed by handlers
// - services: business layer container
//
// Even if some handlers don't need services today, this signature makes it easy to add
// handlers that do later without changing wiring patterns.
func NewHandlers(s *server.Server, services *service.Services) *Handlers {
	return &Handlers{
		Health:  NewHealthHandler(s),
		OpenAPI: NewOpenAPIHandler(s),
	}
}
