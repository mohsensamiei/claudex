package api

import (
	"github.com/gofiber/contrib/otelfiber"
	"github.com/gofiber/fiber/v2"
	fiberSwagger "github.com/gofiber/swagger"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"

	"github.com/leeaandrob/claudex/docs"
	"github.com/leeaandrob/claudex/internal/api/handlers"
	"github.com/leeaandrob/claudex/internal/api/middleware"
	"github.com/leeaandrob/claudex/internal/claude"
	"github.com/leeaandrob/claudex/internal/converter"
	"github.com/leeaandrob/claudex/internal/mcp"
	"github.com/leeaandrob/claudex/internal/observability"
)

// RegisterRoutes registers all API routes.
//
// apiKey, when non-empty, enables API-key authentication on the /v1 routes
// (accepted as either `Authorization: Bearer <key>` or `x-api-key: <key>`). An
// empty apiKey leaves the API open (default behaviour).
func RegisterRoutes(app *fiber.App, logger *observability.Logger, metrics *observability.Metrics, executor *claude.Executor, mcpManager *mcp.Manager, apiKey string) {
	// Operational endpoints (health probes + metrics) are registered first so
	// they short-circuit before the tracing/logging middleware below and don't
	// pollute traces, logs, or request metrics with probe traffic.
	health := handlers.NewHealthHandler(executor)
	app.Get("/livez", health.Livez)
	app.Get("/readyz", health.Readyz)
	app.Get("/healthz", health.Healthz)

	// Prometheus metrics endpoint
	app.Get("/metrics", func(c *fiber.Ctx) error {
		fasthttpadaptor.NewFastHTTPHandler(promhttp.Handler())(c.Context())
		return nil
	})

	// Add OpenTelemetry middleware
	app.Use(otelfiber.Middleware(
		otelfiber.WithServerName("openai-claude-proxy"),
	))

	// Add request ID middleware
	app.Use(middleware.RequestID())

	// Add logging middleware
	app.Use(middleware.Logging(logger))

	// OpenAPI 3 spec (hand-maintained) + Swagger UI pointed at it.
	app.Get("/openapi.yaml", func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "application/yaml")
		return c.Send(docs.OpenAPISpec)
	})
	app.Get("/swagger/*", fiberSwagger.New(fiberSwagger.Config{URL: "/openapi.yaml"}))

	// Create handlers. The Anthropic messages handler reuses the chat handler's
	// Claude CLI execution pipeline.
	parser := claude.NewParser()
	conv := converter.NewConverter()
	chatHandler := handlers.NewChatCompletionsHandler(executor, parser, conv, mcpManager, metrics, logger)
	messagesHandler := handlers.NewMessagesHandler(chatHandler)

	// API routes. Auth is scoped to the /v1 group so the operational endpoints
	// (health probes, metrics, swagger) stay open.
	v1 := app.Group("/v1", middleware.APIKeyAuth(apiKey))

	// Native Anthropic Messages API: Claude-format request in, Claude-format out.
	v1.Post("/messages", messagesHandler.Handle)

	// OpenAI-compatible chat completions.
	v1.Post("/chat/completions", chatHandler.Handle)

	// Models endpoint (hardcoded catalog).
	v1.Get("/models", handlers.ListModels)
}
