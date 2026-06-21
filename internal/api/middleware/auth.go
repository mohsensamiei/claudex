package middleware

import (
	"crypto/subtle"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/leeaandrob/claudex/internal/models"
)

const bearerPrefix = "Bearer "

// apiKeyHeader is the Anthropic-style authentication header. The native
// Messages API (and the Anthropic SDKs) authenticate with `x-api-key`, whereas
// the OpenAI surface uses `Authorization: Bearer`. The proxy accepts either.
const apiKeyHeader = "x-api-key"

// APIKeyAuth returns middleware that gates requests behind a static API key.
// The expected key is read from configuration (typically the CLAUDEX_API_KEY
// environment variable).
//
// The key may be presented either as `Authorization: Bearer <key>` (OpenAI
// convention) or `x-api-key: <key>` (Anthropic convention), so OpenAI- and
// Anthropic-format clients both work unchanged.
//
// When apiKey is empty, authentication is disabled and every request passes
// through unchanged, preserving the proxy's default open behaviour.
func APIKeyAuth(apiKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Auth disabled: no key configured.
		if apiKey == "" {
			return c.Next()
		}

		provided, ok := extractAPIKey(c)
		if !ok {
			return unauthorized(c, "Missing API key. Provide 'Authorization: Bearer <key>' or 'x-api-key: <key>'.")
		}

		// Constant-time comparison to avoid leaking the key via timing.
		if subtle.ConstantTimeCompare([]byte(provided), []byte(apiKey)) != 1 {
			return unauthorized(c, "Invalid API key.")
		}

		return c.Next()
	}
}

// extractAPIKey pulls the API key from either the Authorization bearer header
// or the x-api-key header. It returns false when neither carries a key.
func extractAPIKey(c *fiber.Ctx) (string, bool) {
	if header := c.Get(fiber.HeaderAuthorization); strings.HasPrefix(header, bearerPrefix) {
		return strings.TrimPrefix(header, bearerPrefix), true
	}
	if key := c.Get(apiKeyHeader); key != "" {
		return key, true
	}
	return "", false
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
