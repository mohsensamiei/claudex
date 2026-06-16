package api

import (
	"github.com/gofiber/contrib/otelfiber"
	"github.com/gofiber/fiber/v2"
	fiberSwagger "github.com/gofiber/swagger"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"

	"github.com/leeaandrob/claudex/internal/api/handlers"
	"github.com/leeaandrob/claudex/internal/api/middleware"
	"github.com/leeaandrob/claudex/internal/claude"
	"github.com/leeaandrob/claudex/internal/converter"
	"github.com/leeaandrob/claudex/internal/mcp"
	"github.com/leeaandrob/claudex/internal/observability"

	// Register generated swagger docs.
	_ "github.com/leeaandrob/claudex/docs"
)

// RegisterRoutes registers all API routes.
func RegisterRoutes(app *fiber.App, logger *observability.Logger, metrics *observability.Metrics, executor *claude.Executor, mcpManager *mcp.Manager) {
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

	// Swagger UI and JSON spec
	app.Get("/swagger/*", fiberSwagger.HandlerDefault)

	// Create chat completions handler
	parser := claude.NewParser()
	conv := converter.NewConverter()
	chatHandler := handlers.NewChatCompletionsHandler(executor, parser, conv, mcpManager, metrics, logger)

	// API routes
	v1 := app.Group("/v1")
	v1.Post("/chat/completions", chatHandler.Handle)

	// Models endpoints (hardcoded catalog)
	v1.Get("/models", handlers.ListModels)
	v1.Get("/models/:model", handlers.GetModel)

	// MCP tools endpoint (for debugging/discovery)
	v1.Get("/mcp/tools", func(c *fiber.Ctx) error {
		tools := mcpManager.GetAllTools()
		return c.JSON(fiber.Map{
			"tools": tools,
			"count": len(tools),
		})
	})

	// MCP servers endpoint (for debugging/discovery)
	v1.Get("/mcp/servers", func(c *fiber.Ctx) error {
		clients := mcpManager.GetClients()
		return c.JSON(fiber.Map{
			"servers": clients,
			"count":   len(clients),
		})
	})
}
