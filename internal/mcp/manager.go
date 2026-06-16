package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/leeaandrob/claudex/internal/models"
)

// Manager manages multiple MCP clients.
type Manager struct {
	clients      map[string]*Client
	tools        []models.MCPTool
	toolToClient map[string]string // tool name -> client name
	config       *models.MCPConfig
	settings     models.MCPSettings
	mu           sync.RWMutex
}

// NewManager creates a new MCP manager.
func NewManager() *Manager {
	return &Manager{
		clients:      make(map[string]*Client),
		tools:        []models.MCPTool{},
		toolToClient: make(map[string]string),
		settings: models.MCPSettings{
			InitTimeout: 30,
			CallTimeout: 60,
			AutoRestart: true,
			MaxRestarts: 3,
		},
	}
}

// LoadConfig loads MCP configuration from a file.
func (m *Manager) LoadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config models.MCPConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = &config
	m.settings = config.MCP.Settings

	// Apply defaults if not set
	if m.settings.InitTimeout <= 0 {
		m.settings.InitTimeout = 30
	}
	if m.settings.CallTimeout <= 0 {
		m.settings.CallTimeout = 60
	}
	if m.settings.MaxRestarts <= 0 {
		m.settings.MaxRestarts = 3
	}

	return nil
}

// LoadConfigFromEnv loads MCP configuration from environment variable.
func (m *Manager) LoadConfigFromEnv() error {
	configPath := os.Getenv("CLAUDEX_MCP_CONFIG_PATH")
	if configPath == "" {
		// Try default locations
		candidates := []string{
			"claudex.yaml",
			"config/claudex.yaml",
			"/etc/claudex/claudex.yaml",
		}
		for _, path := range candidates {
			if _, err := os.Stat(path); err == nil {
				configPath = path
				break
			}
		}
	}

	if configPath == "" {
		// No config file found, but this is OK - MCP is optional
		return nil
	}

	return m.LoadConfig(configPath)
}

// StartAll starts all enabled MCP servers.
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.config == nil {
		return nil // No config loaded, nothing to start
	}

	for _, serverConfig := range m.config.MCP.Servers {
		if !serverConfig.Enabled {
			continue
		}

		client := NewClient(serverConfig.Name)
		client.SetTimeouts(
			time.Duration(m.settings.InitTimeout)*time.Second,
			time.Duration(m.settings.CallTimeout)*time.Second,
		)

		// Expand environment variables in command and args
		command := os.ExpandEnv(serverConfig.Command)
		args := make([]string, len(serverConfig.Args))
		for i, arg := range serverConfig.Args {
			args[i] = os.ExpandEnv(arg)
		}

		// Expand environment variables in env map
		env := make(map[string]string)
		for k, v := range serverConfig.Env {
			env[k] = os.ExpandEnv(v)
		}

		if err := client.Start(ctx, command, args, env); err != nil {
			// Log error but continue with other servers
			fmt.Fprintf(os.Stderr, "Failed to start MCP server %s: %v\n", serverConfig.Name, err)
			continue
		}

		m.clients[serverConfig.Name] = client

		// Aggregate tools from this client
		for _, tool := range client.GetTools() {
			m.tools = append(m.tools, tool)
			m.toolToClient[tool.Name] = serverConfig.Name
		}
	}

	return nil
}

// StartServer starts a specific MCP server by name.
func (m *Manager) StartServer(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.config == nil {
		return fmt.Errorf("no config loaded")
	}

	// Find server config
	var serverConfig *models.MCPServerConfig
	for i := range m.config.MCP.Servers {
		if m.config.MCP.Servers[i].Name == name {
			serverConfig = &m.config.MCP.Servers[i]
			break
		}
	}

	if serverConfig == nil {
		return fmt.Errorf("server %s not found in config", name)
	}

	// Check if already running
	if _, exists := m.clients[name]; exists {
		return fmt.Errorf("server %s is already running", name)
	}

	client := NewClient(serverConfig.Name)
	client.SetTimeouts(
		time.Duration(m.settings.InitTimeout)*time.Second,
		time.Duration(m.settings.CallTimeout)*time.Second,
	)

	command := os.ExpandEnv(serverConfig.Command)
	args := make([]string, len(serverConfig.Args))
	for i, arg := range serverConfig.Args {
		args[i] = os.ExpandEnv(arg)
	}

	env := make(map[string]string)
	for k, v := range serverConfig.Env {
		env[k] = os.ExpandEnv(v)
	}

	if err := client.Start(ctx, command, args, env); err != nil {
		return fmt.Errorf("failed to start server %s: %w", name, err)
	}

	m.clients[name] = client

	// Add tools from this client
	for _, tool := range client.GetTools() {
		m.tools = append(m.tools, tool)
		m.toolToClient[tool.Name] = name
	}

	return nil
}

// StopAll stops all running MCP servers.
func (m *Manager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for name, client := range m.clients {
		if err := client.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error stopping MCP server %s: %v\n", name, err)
			lastErr = err
		}
	}

	m.clients = make(map[string]*Client)
	m.tools = []models.MCPTool{}
	m.toolToClient = make(map[string]string)

	return lastErr
}

// StopServer stops a specific MCP server by name.
func (m *Manager) StopServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, exists := m.clients[name]
	if !exists {
		return fmt.Errorf("server %s not found", name)
	}

	if err := client.Close(); err != nil {
		return fmt.Errorf("failed to stop server %s: %w", name, err)
	}

	delete(m.clients, name)

	// Remove tools from this server
	var newTools []models.MCPTool
	for _, tool := range m.tools {
		if tool.ServerName != name {
			newTools = append(newTools, tool)
		} else {
			delete(m.toolToClient, tool.Name)
		}
	}
	m.tools = newTools

	return nil
}

// GetAllTools returns all tools from all connected MCP servers.
func (m *Manager) GetAllTools() []models.MCPTool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tools
}

// GetToolsAsOpenAI returns all MCP tools in OpenAI tool format.
func (m *Manager) GetToolsAsOpenAI() []models.Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return models.ToOpenAITools(m.tools)
}

// HasTools returns whether any MCP tools are available.
func (m *Manager) HasTools() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tools) > 0
}

// GetTool returns a specific tool by name.
func (m *Manager) GetTool(name string) (*models.MCPTool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.tools {
		if m.tools[i].Name == name {
			return &m.tools[i], true
		}
	}
	return nil, false
}

// CallTool executes a tool by name, routing to the correct MCP server.
func (m *Manager) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*models.MCPToolResult, error) {
	m.mu.RLock()
	clientName, exists := m.toolToClient[name]
	if !exists {
		m.mu.RUnlock()
		return nil, fmt.Errorf("tool %s not found", name)
	}

	client, clientExists := m.clients[clientName]
	m.mu.RUnlock()

	if !clientExists {
		return nil, fmt.Errorf("client %s not found for tool %s", clientName, name)
	}

	return client.CallTool(ctx, name, arguments)
}

// GetClientCount returns the number of connected MCP clients.
func (m *Manager) GetClientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

// GetClients returns information about all connected clients.
func (m *Manager) GetClients() map[string]models.MCPImplementationInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]models.MCPImplementationInfo)
	for name, client := range m.clients {
		result[name] = client.GetServerInfo()
	}
	return result
}

// IsToolAvailable checks if a tool is available.
func (m *Manager) IsToolAvailable(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.toolToClient[name]
	return exists
}
