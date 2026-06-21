# Claudex

[![CI](https://github.com/leeaandrob/claudex/actions/workflows/ci.yml/badge.svg)](https://github.com/leeaandrob/claudex/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/leeaandrob/claudex)](https://goreportcard.com/report/github.com/leeaandrob/claudex)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/)

**OpenAI-compatible Chat Completions API with MCP tool support.**

Claudex is a lightweight proxy that exposes an OpenAI-compatible Chat Completions API. Drop-in replacement for OpenAI API - works with existing SDKs, tools, and integrations. Supports tool calling, vision, and MCP (Model Context Protocol) servers.

## Features

- **OpenAI Compatible** - Use existing OpenAI SDK code without changes
- **Tool Calling** - Full function calling support with JSON schema validation
- **Vision Support** - Process images via base64 data URLs
- **MCP Integration** - Connect external tool servers via Model Context Protocol
- **Real-time Streaming** - Full SSE support with token-by-token delivery
- **OpenAPI Docs** - Interactive Swagger UI served at `/swagger/index.html`
- **Production Ready** - OpenTelemetry tracing, Prometheus metrics, structured logging
- **Kubernetes Native** - Health checks, graceful shutdown, easy deployment

## Quick Start

### Prerequisites

- Go 1.22+
- Authenticated CLI credentials

### Installation

```bash
# Clone the repository
git clone https://github.com/leeaandrob/claudex.git
cd claudex

# Build and run
make run
```

### Basic Usage

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="not-needed"
)

# Simple chat
response = client.chat.completions.create(
    model="claude-sonnet",
    messages=[
        {"role": "system", "content": "You are a helpful assistant."},
        {"role": "user", "content": "Hello!"}
    ]
)
print(response.choices[0].message.content)

# Streaming
stream = client.chat.completions.create(
    model="claude-sonnet",
    messages=[{"role": "user", "content": "Tell me a story"}],
    stream=True
)
for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="")
```

### Listing Models

```python
# List available models
models = client.models.list()
for model in models.data:
    print(model.id)
```

```bash
curl http://localhost:8080/v1/models
```

### Tool Calling

```python
tools = [
    {
        "type": "function",
        "function": {
            "name": "get_weather",
            "description": "Get current weather for a location",
            "parameters": {
                "type": "object",
                "properties": {
                    "location": {"type": "string", "description": "City name"}
                },
                "required": ["location"]
            }
        }
    }
]

response = client.chat.completions.create(
    model="claude-sonnet",
    messages=[{"role": "user", "content": "What's the weather in Tokyo?"}],
    tools=tools
)

# Handle tool calls
if response.choices[0].message.tool_calls:
    for tool_call in response.choices[0].message.tool_calls:
        print(f"Tool: {tool_call.function.name}")
        print(f"Args: {tool_call.function.arguments}")
```

### Vision Support

```python
import base64

with open("image.png", "rb") as f:
    image_data = base64.b64encode(f.read()).decode()

response = client.chat.completions.create(
    model="claude-sonnet",
    messages=[
        {
            "role": "user",
            "content": [
                {"type": "text", "text": "What's in this image?"},
                {
                    "type": "image_url",
                    "image_url": {"url": f"data:image/png;base64,{image_data}"}
                }
            ]
        }
    ]
)
```

## Anthropic-Native API

Claudex also exposes a native [Anthropic Messages API](https://docs.anthropic.com/en/api/messages)
endpoint at `POST /v1/messages`. It accepts requests in Claude format and responds
in Claude format, so the official Anthropic SDKs work by pointing them at Claudex:

```python
from anthropic import Anthropic

client = Anthropic(base_url="http://localhost:8080", api_key="not-needed")

message = client.messages.create(
    model="claude-sonnet",
    max_tokens=1024,
    system="You are a helpful assistant.",
    messages=[{"role": "user", "content": "Hello!"}],
)
print(message.content[0].text)
```

```bash
curl http://localhost:8080/v1/messages \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

Streaming (`"stream": true`), tool use (`tools` / `tool_use` / `tool_result`
blocks), and vision (base64 `image` blocks) are all supported in the native
format. The OpenAI-compatible surface remains available at
`/v1/chat/completions`.

When authentication is enabled, this endpoint follows the Anthropic convention
and authenticates with the `x-api-key` header (the OpenAI endpoints use
`Authorization: Bearer`). The proxy accepts either header on any `/v1` route.

## MCP Server Integration

Claudex supports [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) servers, allowing you to extend capabilities with external tools.

### Configuration

Create `config/claudex.yaml`:

```yaml
mcp:
  settings:
    init_timeout: 30    # Seconds to wait for server initialization
    call_timeout: 60    # Seconds to wait for tool execution
    auto_restart: true  # Restart failed servers automatically
    max_restarts: 3     # Maximum restart attempts

  servers:
    - name: my-tools
      enabled: true
      command: "/path/to/mcp-server"
      args:
        - "--option"
        - "value"
      env:
        API_KEY: "${MY_API_KEY}"
```

### Running with MCP

```bash
CLAUDEX_MCP_CONFIG_PATH=config/claudex.yaml ./server
```

MCP tools are automatically available in chat completions when configured.

## API Reference

Interactive API documentation is available via Swagger UI at
[http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html)
once the server is running. The raw OpenAPI 3 spec (hand-maintained in
`docs/openapi.yaml`) is served at `/openapi.yaml`.

### Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/messages` | POST | **Anthropic-native** Messages API: Claude-format request in, Claude-format response out (streaming, tool use, vision). Auth: `x-api-key` |
| `/v1/chat/completions` | POST | OpenAI-compatible chat completions. Auth: `Authorization: Bearer` |
| `/v1/models` | GET | List available model names |
| `/livez` | GET | Liveness probe |
| `/readyz` | GET | Readiness probe (200 when Claude CLI is available, else 503) |
| `/healthz` | GET | Health check (200 when healthy, else 503) |
| `/metrics` | GET | Prometheus metrics |
| `/swagger/index.html` | GET | Interactive Swagger UI |

### Authentication

By default the API is open. Set the `CLAUDEX_API_KEY` environment variable to
require an API key on the `/v1/*` routes:

```bash
CLAUDEX_API_KEY=my-secret-key ./claudex
```

The key may be presented with either convention, so OpenAI- and Anthropic-format
clients both work unchanged:

```bash
# OpenAI convention (works with any OpenAI SDK via the api_key field)
curl http://localhost:8080/v1/models \
  -H "Authorization: Bearer my-secret-key"

# Anthropic convention (works with the Anthropic SDKs)
curl http://localhost:8080/v1/messages \
  -H "x-api-key: my-secret-key" \
  -H "content-type: application/json" \
  -d '{"model":"claude-sonnet","max_tokens":256,"messages":[{"role":"user","content":"Hi"}]}'
```

Requests without a valid key receive `401 Unauthorized`. The operational
endpoints (`/livez`, `/readyz`, `/healthz`, `/metrics`, `/swagger/*`) stay open
regardless, so health probes and metrics scraping keep working.

### Compatibility Matrix

| Feature | Status |
|---------|--------|
| Chat Completions | ✅ |
| Streaming (SSE) | ✅ |
| System messages | ✅ |
| Multi-turn conversations | ✅ |
| Tool calling | ✅ |
| Vision (images) | ✅ |
| MCP tools | ✅ |

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `CLAUDEX_API_KEY` | - | When set, requires an API key on `/v1/*` routes via `Authorization: Bearer <key>` or `x-api-key: <key>`. Unset = open access (default) |
| `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `REQUEST_TIMEOUT` | `600` | Request timeout in seconds |
| `CLAUDEX_MCP_CONFIG_PATH` | - | Path to MCP configuration file |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | - | OpenTelemetry endpoint |
| `SERVICE_NAME` | `claudex` | Service name for tracing |

## Deployment

> **Token refresh (read this first).** Claudex shells out to the Claude CLI,
> which auto-refreshes the access token using the `refreshToken` and writes the
> new token back to `~/.claude/.credentials.json`. For that to survive, the
> `.claude` directory inside the container **must be a writable, persistent
> volume** — a read-only mount (`:ro`) or a read-only Kubernetes secret blocks
> the write, so the token expires after a few hours and you're forced to log in
> again. Always mount the **directory** (not the single file), read-write, and
> seed it once with a full credentials JSON that contains a `refreshToken`.

### Docker

```bash
# Build for your architecture
docker build -f Dockerfile.amd64 -t claudex .  # For x86_64
docker build -f Dockerfile -t claudex .         # For ARM64

# Seed a dedicated, writable credentials dir once
mkdir -p ~/.claudex-creds
cp ~/.claude/.credentials.json ~/.claudex-creds/.credentials.json

# Run with a writable .claude dir so the CLI can persist refreshed tokens
docker run -p 8080:8080 \
  -v ~/.claudex-creds:/home/appuser/.claude \
  claudex
```

### Docker Compose

```bash
# Seed a dedicated, writable credentials dir once
mkdir -p ./.claudex
cp ~/.claude/.credentials.json ./.claudex/.credentials.json

# Run (see the volume mapping in docker-compose.yml)
docker-compose up -d
```

### Kubernetes

Kubernetes secret mounts are always read-only, so the CLI cannot refresh into
them. Mount a **writable PVC** at `~/.claude` and seed it from the secret on
first start via `CLAUDE_CREDENTIALS_SEED` (the entrypoint copies it only when no
credentials exist yet, so refreshed tokens survive restarts).

```bash
# Create secret with the full credentials JSON (must include refreshToken)
kubectl create secret generic claude-credentials \
  --from-file=.credentials.json=$HOME/.claude/.credentials.json
```

```yaml
# Pod/Deployment spec excerpt
spec:
  containers:
    - name: claudex
      image: claudex
      env:
        - name: CLAUDE_CREDENTIALS_SEED
          value: /secrets/.credentials.json
      volumeMounts:
        - name: claude-home          # writable, persistent
          mountPath: /home/appuser/.claude
        - name: claude-secret        # read-only seed source
          mountPath: /secrets
          readOnly: true
  volumes:
    - name: claude-home
      persistentVolumeClaim:
        claimName: claudex-claude-home
    - name: claude-secret
      secret:
        secretName: claude-credentials
```

> If the PVC is lost (or you use an `emptyDir`), the pod re-seeds from the secret
> on restart — which only works while that secret's token still has a valid
> `refreshToken`. A PVC is recommended so the long-lived refreshed credentials
> persist across restarts.

## Development

```bash
make build       # Build binary
make test        # Run unit tests
make test-e2e    # Run E2E tests
make lint        # Run linter
make fmt         # Format code
make vet         # Run go vet
make swagger     # Regenerate OpenAPI docs from annotations
make clean       # Clean build artifacts
```

> Regenerating docs requires the `swag` CLI:
> `go install github.com/swaggo/swag/cmd/swag@latest`

### Building Multi-Architecture Binaries

```bash
# Linux AMD64
GOOS=linux GOARCH=amd64 go build -o server-amd64 ./cmd/server

# Linux ARM64
GOOS=linux GOARCH=arm64 go build -o server-arm64 ./cmd/server
```

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────┐
│  OpenAI Client  │────▶│     Claudex     │────▶│     CLI     │
│  (Python SDK)   │◀────│   (Go + Fiber)  │◀────│  (Backend)  │
└─────────────────┘     └─────────────────┘     └─────────────┘
                               │
                               │  ┌─────────────────┐
                               ├──│   MCP Server 1  │
                               │  └─────────────────┘
                               │  ┌─────────────────┐
                               ├──│   MCP Server 2  │
                               │  └─────────────────┘
                               │
                               ├── OpenTelemetry Traces
                               ├── Prometheus Metrics
                               └── Structured Logs (JSON)
```

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details.

## License

[MIT](LICENSE) - Use it freely in your projects.
