package handler

// HealthHandler exposes a "system" endpoint that external systems can use to verify
// the service is alive and dependencies are reachable.
//
// Tutor intent (03:07:25–03:11:25):
// - Backend systems should expose a health endpoint so Kubernetes / uptime monitors / load balancers
//   can check whether the service is running.
// - It should return a successful response when the service is healthy.
// - It can also report sub-checks like database and Redis connectivity.
import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/deppfellow/go-boilerplate/internal/middleware"
	"github.com/deppfellow/go-boilerplate/internal/server"
	"github.com/labstack/echo/v4"
)

// HealthHandler embeds the base Handler to reuse shared server dependencies.
// This endpoint is not "business logic", but embedding keeps handler patterns consistent.
type HealthHandler struct {
	Handler
}

// NewHealthHandler constructs a HealthHandler with access to shared app dependencies.
func NewHealthHandler(s *server.Server) *HealthHandler {
	return &HealthHandler{
		Handler: NewHandler(s),
	}
}

// CheckHealth returns system health status and dependency checks.
//
// Response includes:
// - overall status (healthy/unhealthy)
// - timestamp (UTC)
// - environment (from config)
// - checks map (database, redis)
//
// It returns:
// - 200 OK if all checks pass
// - 503 Service Unavailable if any check fails
func (h *HealthHandler) CheckHealth(c echo.Context) error {
	start := time.Now()

	// Use the request-scoped logger from context enhancer middleware.
	logger := middleware.GetLogger(c).With().
		Str("operation", "health_check").
		Logger()

	// Base response format.
	response := map[string]interface{}{
		"status":      "healthy",
		"timestamp":   time.Now().UTC(),
		"environment": h.server.Config.Primary.Env,
		"checks":      make(map[string]interface{}),
	}

	checks := response["checks"].(map[string]interface{})
	isHealthy := true

	// ---------------- Database connectivity check ----------------------------
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dbStart := time.Now()

	// pgxpool provides Ping to verify connectivity.
	if err := h.server.DB.Pool.Ping(ctx); err != nil {
		checks["database"] = map[string]interface{}{
			"status":        "unhealthy",
			"response_time": time.Since(dbStart).String(),
			"error":         err.Error(),
		}

		isHealthy = false

		logger.Error().
			Err(err).
			Dur("response_time", time.Since(dbStart)).
			Msg("database health check failed")

		// Record a New Relic custom event if enabled.
		if h.server.LoggerService != nil && h.server.LoggerService.GetApplication() != nil {
			h.server.LoggerService.GetApplication().RecordCustomEvent(
				"HealthCheckError",
				map[string]interface{}{
					"check_type":       "database",
					"operation":        "health_check",
					"error_type":       "database_unhealthy",
					"response_time_ms": time.Since(dbStart).Milliseconds(),
					"error_message":    err.Error(),
				},
			)
		}
	} else {
		checks["database"] = map[string]interface{}{
			"status":        "healthy",
			"response_time": time.Since(dbStart).String(),
		}

		logger.Info().
			Dur("response_time", time.Since(dbStart)).
			Msg("database health check passed")
	}

	// Note: DB connection metrics/traces are automatically captured by New Relic nrpgx5 integration.

	// ---------------- Redis connectivity check -------------------------------
	// Tutor mentions checking "Redis connectivity" as part of health status.
	if h.server.Redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		redisStart := time.Now()

		if err := h.server.Redis.Ping(ctx).Err(); err != nil {
			checks["redis"] = map[string]interface{}{
				"status":        "unhealthy",
				"response_time": time.Since(redisStart).String(),
				"error":         err.Error(),
			}

			// NOTE: In your current code, you do NOT set isHealthy=false here.
			// That means Redis can be unhealthy and the endpoint may still return 200.
			// If Redis is a required dependency, set isHealthy = false here.
			// isHealthy = false

			logger.Error().
				Err(err).
				Dur("response_time", time.Since(redisStart)).
				Msg("redis health check failed")

			if h.server.LoggerService != nil && h.server.LoggerService.GetApplication() != nil {
				h.server.LoggerService.GetApplication().RecordCustomEvent(
					"HealthCheckError",
					map[string]interface{}{
						"check_type":       "redis",
						"operation":        "health_check",
						"error_type":       "redis_unhealthy",
						"response_time_ms": time.Since(redisStart).Milliseconds(),
						"error_message":    err.Error(),
					},
				)
			}
		} else {
			checks["redis"] = map[string]interface{}{
				"status":        "healthy",
				"response_time": time.Since(redisStart).String(),
			}

			logger.Info().
				Dur("response_time", time.Since(redisStart)).
				Msg("redis health check passed")
		}
	}

	// ---------------- Overall status + response ------------------------------
	if !isHealthy {
		response["status"] = "unhealthy"

		logger.Warn().
			Dur("total_duration", time.Since(start)).
			Msg("health check failed")

		if h.server.LoggerService != nil && h.server.LoggerService.GetApplication() != nil {
			h.server.LoggerService.GetApplication().RecordCustomEvent(
				"HealthCheckError",
				map[string]interface{}{
					"check_type":        "overall",
					"operation":         "health_check",
					"error_type":        "overall_unhealthy",
					"total_duration_ms": time.Since(start).Milliseconds(),
				},
			)
		}

		return c.JSON(http.StatusServiceUnavailable, response)
	}

	logger.Info().
		Dur("total_duration", time.Since(start)).
		Msg("health check passed")

	// If JSON write fails, record telemetry and return a wrapped error.
	if err := c.JSON(http.StatusOK, response); err != nil {
		logger.Error().Err(err).Msg("failed to write JSON response")

		if h.server.LoggerService != nil && h.server.LoggerService.GetApplication() != nil {
			h.server.LoggerService.GetApplication().RecordCustomEvent(
				"HealthCheckError",
				map[string]interface{}{
					"check_type":    "response",
					"operation":     "health_check",
					"error_type":    "json_response_error",
					"error_message": err.Error(),
				},
			)
		}

		return fmt.Errorf("failed to write JSON response: %w", err)
	}

	return nil
}
