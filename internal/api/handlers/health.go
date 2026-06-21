package handlers

import (
	"github.com/gofiber/fiber/v2"

	"github.com/leeaandrob/claudex/internal/claude"
)

// HealthResponse is the response body returned by the health and probe endpoints.
type HealthResponse struct {
	Status    string `json:"status" example:"ok"`
	ClaudeCLI bool   `json:"claude_cli" example:"true"`
}

// HealthHandler serves liveness, readiness, and general health endpoints.
type HealthHandler struct {
	executor *claude.Executor
}

// NewHealthHandler creates a new health handler.
func NewHealthHandler(executor *claude.Executor) *HealthHandler {
	return &HealthHandler{executor: executor}
}

// Healthz reports the overall health of the service: 200 when the Claude CLI is
// available, otherwise 503.
func (h *HealthHandler) Healthz(c *fiber.Ctx) error {
	if !h.executor.IsAvailable() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(HealthResponse{
			Status:    "unavailable",
			ClaudeCLI: false,
		})
	}
	return c.JSON(HealthResponse{Status: "ok", ClaudeCLI: true})
}
