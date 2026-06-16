package middleware

import (
	"crypto/subtle"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/leeaandrob/claudex/internal/models"
)

const bearerPrefix = "Bearer "

// APIKeyAuth returns middleware that gates requests behind a static bearer
// token. The expected key is read from configuration (typically the
// CLAUDEX_API_KEY environment variable).
//
// When apiKey is empty, authentication is disabled and every request passes
// through unchanged, preserving the proxy's default open behaviour.
func APIKeyAuth(apiKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Auth disabled: no key configured.
		if apiKey == "" {
			return c.Next()
		}

		header := c.Get(fiber.HeaderAuthorization)
		if !strings.HasPrefix(header, bearerPrefix) {
			return unauthorized(c, "Missing or malformed Authorization header. Expected 'Authorization: Bearer <key>'.")
		}

		provided := strings.TrimPrefix(header, bearerPrefix)
		// Constant-time comparison to avoid leaking the key via timing.
		if subtle.ConstantTimeCompare([]byte(provided), []byte(apiKey)) != 1 {
			return unauthorized(c, "Invalid API key.")
		}

		return c.Next()
	}
}

func unauthorized(c *fiber.Ctx, message string) error {
	return c.Status(fiber.StatusUnauthorized).JSON(models.ErrorResponse{
		Error: models.ErrorDetail{
			Message: message,
			Type:    "invalid_request_error",
			Code:    "invalid_api_key",
		},
	})
}
