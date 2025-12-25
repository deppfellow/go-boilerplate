package handler

import (
	"fmt"
	"net/http"
	"os"

	"github.com/deppfellow/go-boilerplate/internal/server"
	"github.com/labstack/echo/v4"
)

// OpenAPIHandler serves the OpenAPI UI for testing APIs.
//
// Tutor intent (03:11:56–03:13:55):
// - Provide a simple UI to test APIs.
// - The UI is a static HTML file (openapi.html) that loads JS from a CDN.
// - It reads an OpenAPI JSON file from the static folder (e.g., openapi.json).
// - The handler reads the HTML template and serves it as HTML.
// - Disable caching to ensure updates to docs appear immediately during development.
type OpenAPIHandler struct {
	Handler
}

// NewOpenAPIHandler constructs an OpenAPIHandler with access to shared dependencies.
func NewOpenAPIHandler(s *server.Server) *OpenAPIHandler {
	return &OpenAPIHandler{
		Handler: NewHandler(s),
	}
}

// ServeOpenAPIUI reads static/openapi.html and serves it as an HTML response.
//
// Cache-Control is set to "no-cache" so clients do not reuse old docs UI.
func (h *OpenAPIHandler) ServeOpenAPIUI(c echo.Context) error {
	templateBytes, err := os.ReadFile("static/openapi.html")

	// Prevent caching of the docs UI page.
	c.Response().Header().Set("Cache-Control", "no-cache")

	if err != nil {
		return fmt.Errorf("failed to read OpenAPI UI template: %w", err)
	}

	templateString := string(templateBytes)

	if err := c.HTML(http.StatusOK, templateString); err != nil {
		return fmt.Errorf("failed to write HTML response: %w", err)
	}

	return nil
}
