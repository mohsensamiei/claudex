package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/leeaandrob/claudex/internal/api"
	"github.com/leeaandrob/claudex/internal/claude"
	"github.com/leeaandrob/claudex/internal/mcp"
	"github.com/leeaandrob/claudex/internal/observability"
)

//	@title			Claudex API
//	@version		1.0
//	@description	OpenAI-compatible Chat Completions API backed by the Claude CLI, with MCP tool support.
//	@license.name	MIT
//	@license.url	https://opensource.org/licenses/MIT
//	@BasePath		/
//	@schemes		http https

func main() {
	// Configuration from environment
	port := getEnv("PORT", "8080")
	logLevel := getEnv("LOG_LEVEL", "info")
	otlpEndpoint := getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	serviceName := getEnv("SERVICE_NAME", "openai-claude-proxy")
	apiKey := getEnv("CLAUDEX_API_KEY", "")

	// Initialize logger
	logger := observability.NewLogger(logLevel)
	logger.Info("starting server",
		"port", port,
		"log_level", logLevel,
		"otlp_endpoint", otlpEndpoint,
		"api_key_auth", apiKey != "",
	)

	// Initialize tracing (if endpoint configured)
	if otlpEndpoint != "" {
		tp, err := observability.InitTracer(context.Background(), serviceName, otlpEndpoint)
		if err != nil {
			logger.Warn("failed to initialize tracer", "error", err.Error())
		} else {
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := tp.Shutdown(ctx); err != nil {
					logger.Error("failed to shutdown tracer", "error", err.Error())
				}
			}()
			logger.Info("tracer initialized", "endpoint", otlpEndpoint)
		}
	}

	// Initialize metrics
	metrics := observability.InitMetrics()
	logger.Info("metrics initialized")

	// Initialize Claude executor
	executor := claude.NewExecutor()
	if !executor.IsAvailable() {
		logger.Warn("claude CLI is not available, some features may not work")
	} else {
		logger.Info("claude CLI is available")
	}

	// Initialize MCP manager
	mcpManager := mcp.NewManager()
	if err := mcpManager.LoadConfigFromEnv(); err != nil {
		logger.Warn("failed to load MCP config", "error", err.Error())
	}

	// Start MCP servers
	mcpCtx, mcpCancel := context.WithTimeout(context.Background(), 60*time.Second)
	if err := mcpManager.StartAll(mcpCtx); err != nil {
		logger.Warn("failed to start MCP servers", "error", err.Error())
	} else if mcpManager.GetClientCount() > 0 {
		logger.Info("MCP servers started",
			"count", mcpManager.GetClientCount(),
			"tools", len(mcpManager.GetAllTools()))
	}
	mcpCancel()

	// Create Fiber app
	app := fiber.New(fiber.Config{
		AppName:               serviceName,
		DisableStartupMessage: true,
		ReadTimeout:           10 * time.Minute,
		WriteTimeout:          10 * time.Minute,
	})

	// Add recover middleware
	app.Use(recover.New())

	// Register routes
	api.RegisterRoutes(app, logger, metrics, executor, mcpManager, apiKey)

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		sig := <-sigCh

		logger.Info("received shutdown signal", "signal", sig.String())

		// Stop MCP servers
		if err := mcpManager.StopAll(); err != nil {
			logger.Error("error stopping MCP servers", "error", err.Error())
		}

		// Give in-flight requests time to complete
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := app.ShutdownWithContext(ctx); err != nil {
			logger.Error("error during shutdown", "error", err.Error())
		}
	}()

	// Start server
	logger.Info("server listening", "port", port)
	if err := app.Listen(":" + port); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

// getEnv returns environment variable value or default.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
