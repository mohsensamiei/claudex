.PHONY: build run test coverage lint clean docker-build fmt vet swagger tidy test-e2e test-e2e-setup check

# Build variables
BINARY_NAME=server
BUILD_DIR=bin
CMD_DIR=cmd/server
MAIN=$(CMD_DIR)/main.go
DOCS_DIR=docs

# Tools
SWAG ?= swag

# Build the binary
build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)

# Run the server
run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

# Run tests with race detection and coverage
test:
	go test -v -race -coverprofile=coverage.out ./...

# Run tests and generate HTML coverage report
coverage: test
	go tool cover -html=coverage.out -o coverage.html

# Format code
fmt:
	go fmt ./...

# Run go vet
vet:
	go vet ./...

# Run linter (requires golangci-lint)
lint:
	golangci-lint run

# Generate Swagger/OpenAPI docs from code annotations.
# Install swag with: go install github.com/swaggo/swag/cmd/swag@latest
swagger:
	@command -v $(SWAG) >/dev/null 2>&1 || { echo "swag not found; install with: go install github.com/swaggo/swag/cmd/swag@latest"; exit 1; }
	$(SWAG) init -g $(MAIN) -o $(DOCS_DIR) --parseDependency --parseInternal

# Tidy go module dependencies
tidy:
	go mod tidy

# Build Docker image
docker-build:
	docker build -t openai-claude-proxy .

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)/ coverage.out coverage.html

# Setup e2e test dependencies (using uv)
test-e2e-setup:
	cd tests/e2e && uv venv && uv pip install -r requirements.txt

# Run e2e tests (requires server to be running)
test-e2e: build
	@echo "Starting server in background..."
	@./$(BUILD_DIR)/$(BINARY_NAME) & echo $$! > .server.pid
	@sleep 2
	@echo "Running e2e tests..."
	@cd tests/e2e && uv run pytest -v; TEST_EXIT=$$?; \
		kill `cat ../../.server.pid` 2>/dev/null; \
		rm -f ../../.server.pid; \
		exit $$TEST_EXIT

# Run all checks before commit
check: fmt vet lint test build
