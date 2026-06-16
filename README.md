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

### MCP API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/mcp/tools` | GET | List all available MCP tools |
| `/v1/mcp/servers` | GET | List connected MCP servers |

MCP tools are automatically available in chat completions when configured.

## API Reference

Interactive API documentation is available via Swagger UI at
[http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html)
once the server is running. The raw OpenAPI spec is served at `/swagger/doc.json`.

### Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/chat/completions` | POST | OpenAI-compatible chat completions |
| `/v1/models` | GET | List available models |
| `/v1/models/{model}` | GET | Retrieve a single model |
| `/v1/mcp/tools` | GET | List MCP tools |
| `/v1/mcp/servers` | GET | List MCP servers |
| `/livez` | GET | Liveness probe |
| `/readyz` | GET | Readiness probe (200 when Claude CLI is available, else 503) |
| `/healthz` | GET | Health check (200 when healthy, else 503) |
| `/metrics` | GET | Prometheus metrics |
| `/swagger/index.html` | GET | Interactive Swagger UI |

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
| `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `REQUEST_TIMEOUT` | `600` | Request timeout in seconds |
| `CLAUDEX_MCP_CONFIG_PATH` | - | Path to MCP configuration file |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | - | OpenTelemetry endpoint |
| `SERVICE_NAME` | `claudex` | Service name for tracing |

## Deployment

### Docker

```bash
# Build for your architecture
docker build -f Dockerfile.amd64 -t claudex .  # For x86_64
docker build -f Dockerfile -t claudex .         # For ARM64

# Run with credentials
docker run -p 8080:8080 \
  -e CLAUDE_CREDENTIALS_BASE64="$(base64 -w0 ~/.claude/credentials.json)" \
  claudex
```

### Docker Compose

```bash
# Set credentials
export CLAUDE_CREDENTIALS_BASE64=$(base64 -w0 ~/.claude/credentials.json)

# Run
docker-compose up -d
```

### Kubernetes

```bash
# Create secret with credentials
kubectl create secret generic claude-credentials \
  --from-file=credentials.json=$HOME/.claude/credentials.json

# Deploy
kubectl apply -f k8s/
```

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
