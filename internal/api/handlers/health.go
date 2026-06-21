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

// Livez reports whether the process is alive.
func (h *HealthHandler) Livez(c *fiber.Ctx) error {
	return c.JSON(HealthResponse{Status: "ok", ClaudeCLI: h.executor.IsAvailable()})
}

// Readyz reports whether the service is ready to serve traffic.
func (h *HealthHandler) Readyz(c *fiber.Ctx) error {
	return h.respond(c)
}

// Healthz reports the overall health of the service.
func (h *HealthHandler) Healthz(c *fiber.Ctx) error {
	return h.respond(c)
}

// respond returns a health payload based on Claude CLI availability.
func (h *HealthHandler) respond(c *fiber.Ctx) error {
	if !h.executor.IsAvailable() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(HealthResponse{
			Status:    "unavailable",
			ClaudeCLI: false,
		})
	}
	return c.JSON(HealthResponse{Status: "ok", ClaudeCLI: true})
}
