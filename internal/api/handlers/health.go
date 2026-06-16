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
//
//	@Summary		Liveness probe
//	@Description	Always returns 200 while the process is running.
//	@Tags			health
//	@Produce		json
//	@Success		200	{object}	HealthResponse
//	@Router			/livez [get]
func (h *HealthHandler) Livez(c *fiber.Ctx) error {
	return c.JSON(HealthResponse{Status: "ok", ClaudeCLI: h.executor.IsAvailable()})
}

// Readyz reports whether the service is ready to serve traffic.
//
//	@Summary		Readiness probe
//	@Description	Returns 200 when the Claude CLI is available, otherwise 503.
//	@Tags			health
//	@Produce		json
//	@Success		200	{object}	HealthResponse
//	@Failure		503	{object}	HealthResponse
//	@Router			/readyz [get]
func (h *HealthHandler) Readyz(c *fiber.Ctx) error {
	return h.respond(c)
}

// Healthz reports the overall health of the service.
//
//	@Summary		Health check
//	@Description	Returns 200 when the service is healthy (Claude CLI available), otherwise 503.
//	@Tags			health
//	@Produce		json
//	@Success		200	{object}	HealthResponse
//	@Failure		503	{object}	HealthResponse
//	@Router			/healthz [get]
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
