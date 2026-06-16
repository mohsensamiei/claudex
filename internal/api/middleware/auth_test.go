package middleware

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func newAuthApp(apiKey string) *fiber.App {
	app := fiber.New()
	app.Use(APIKeyAuth(apiKey))
	app.Get("/v1/ping", func(c *fiber.Ctx) error {
		return c.SendString("pong")
	})
	return app
}

func doGet(t *testing.T, app *fiber.App, authHeader string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/v1/ping", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestAPIKeyAuth_DisabledWhenEmpty(t *testing.T) {
	app := newAuthApp("")
	status, body := doGet(t, app, "")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 when auth disabled, got %d", status)
	}
	if body != "pong" {
		t.Fatalf("expected pong, got %q", body)
	}
}

func TestAPIKeyAuth_ValidKey(t *testing.T) {
	app := newAuthApp("secret")
	status, _ := doGet(t, app, "Bearer secret")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 with valid key, got %d", status)
	}
}

func TestAPIKeyAuth_MissingHeader(t *testing.T) {
	app := newAuthApp("secret")
	status, _ := doGet(t, app, "")
	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 with no header, got %d", status)
	}
}

func TestAPIKeyAuth_WrongKey(t *testing.T) {
	app := newAuthApp("secret")
	status, _ := doGet(t, app, "Bearer wrong")
	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong key, got %d", status)
	}
}

func TestAPIKeyAuth_MalformedHeader(t *testing.T) {
	app := newAuthApp("secret")
	status, _ := doGet(t, app, "secret")
	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 with malformed header, got %d", status)
	}
}
