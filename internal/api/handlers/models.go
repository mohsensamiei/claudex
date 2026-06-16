package handlers

import (
	"github.com/gofiber/fiber/v2"

	"github.com/leeaandrob/claudex/internal/models"
)

// modelsCreatedAt is a fixed creation timestamp (2024-07-01T00:00:00Z) used for
// the hardcoded model catalog so responses are stable across restarts.
const modelsCreatedAt int64 = 1719792000

// supportedModels is the hardcoded catalog of models exposed via /v1/models.
// Claudex proxies requests to the Claude CLI, so these identifiers mirror the
// Claude model families that callers may pass in the `model` field.
var supportedModels = []models.Model{
	{ID: "claude-opus-4-8", Object: "model", Created: modelsCreatedAt, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-6", Object: "model", Created: modelsCreatedAt, OwnedBy: "anthropic"},
	{ID: "claude-haiku-4-5", Object: "model", Created: modelsCreatedAt, OwnedBy: "anthropic"},
	{ID: "claude-opus", Object: "model", Created: modelsCreatedAt, OwnedBy: "anthropic"},
	{ID: "claude-sonnet", Object: "model", Created: modelsCreatedAt, OwnedBy: "anthropic"},
	{ID: "claude-haiku", Object: "model", Created: modelsCreatedAt, OwnedBy: "anthropic"},
}

// ListModels returns the catalog of available models.
//
//	@Summary		List models
//	@Description	Lists the models available through the proxy, in an OpenAI-compatible format.
//	@Tags			models
//	@Produce		json
//	@Success		200	{object}	models.ModelList
//	@Router			/v1/models [get]
func ListModels(c *fiber.Ctx) error {
	return c.JSON(models.ModelList{
		Object: "list",
		Data:   supportedModels,
	})
}

// GetModel returns a single model by ID.
//
//	@Summary		Retrieve a model
//	@Description	Retrieves a single model by its ID, in an OpenAI-compatible format.
//	@Tags			models
//	@Produce		json
//	@Param			model	path		string	true	"Model ID"
//	@Success		200		{object}	models.Model
//	@Failure		404		{object}	models.ErrorResponse
//	@Router			/v1/models/{model} [get]
func GetModel(c *fiber.Ctx) error {
	id := c.Params("model")
	for _, m := range supportedModels {
		if m.ID == id {
			return c.JSON(m)
		}
	}

	return c.Status(fiber.StatusNotFound).JSON(models.ErrorResponse{
		Error: models.ErrorDetail{
			Message: "The model '" + id + "' does not exist",
			Type:    "invalid_request_error",
			Code:    "model_not_found",
		},
	})
}
