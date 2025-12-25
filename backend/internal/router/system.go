package router

import (
	"github.com/deppfellow/go-boilerplate/internal/handler"
	"github.com/labstack/echo/v4"
)

// registerSystemRoutes registers "system" endpoints that are not part of business logic.
//
// Tutor intent (03:14:16–03:14:50):
// - Keep system routes separate in a dedicated file.
// - Routes include:
//  1. Health endpoint
//  2. Docs endpoint (OpenAPI UI)
//  3. Static files endpoint (to serve openapi.json and openapi.html assets)
func registerSystemRoutes(r *echo.Echo, h *handler.Handlers) {
	// Health status endpoint (used by Kubernetes/monitors).
	r.GET("/status", h.Health.CheckHealth)

	// Serve all files from ./static at /static/*.
	// Used for openapi.json and openapi.html (and any future docs assets).
	r.Static("/static", "static")

	// Docs UI endpoint (serves openapi.html).
	r.GET("/docs", h.OpenAPI.ServeOpenAPIUI)
}
