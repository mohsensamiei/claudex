package handlers

import (
	"github.com/gofiber/fiber/v2"
)

// supportedModels is the hardcoded catalog of model names exposed via
// /v1/models. Claudex proxies requests to the Claude CLI, so these identifiers
// mirror the Claude model families that callers may pass in the `model` field.
var supportedModels = []string{
	"claude-opus-4-8",
	"claude-sonnet-4-6",
	"claude-haiku-4-5",
	"claude-opus",
	"claude-sonnet",
	"claude-haiku",
}

// ListModels returns the catalog of available model names (GET /v1/models).
func ListModels(c *fiber.Ctx) error {
	return c.JSON(supportedModels)
}
