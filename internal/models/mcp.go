package models

import (
	"encoding/json"
)

// MCPConfig represents the complete MCP configuration.
type MCPConfig struct {
	MCP MCPSection `yaml:"mcp" json:"mcp"`
}

// MCPSection contains MCP settings and server definitions.
type MCPSection struct {
	Settings MCPSettings       `yaml:"settings" json:"settings"`
	Servers  []MCPServerConfig `yaml:"servers" json:"servers"`
}

// MCPSettings contains global MCP configuration.
type MCPSettings struct {
	InitTimeout int  `yaml:"init_timeout" json:"init_timeout"` // Timeout for MCP server initialization (seconds)
	CallTimeout int  `yaml:"call_timeout" json:"call_timeout"` // Timeout for tool calls (seconds)
	AutoRestart bool `yaml:"auto_restart" json:"auto_restart"` // Restart failed servers automatically
	MaxRestarts int  `yaml:"max_restarts" json:"max_restarts"` // Max restart attempts before giving up
}

// MCPServerConfig represents a single MCP server configuration.
type MCPServerConfig struct {
	Name    string            `yaml:"name" json:"name"`
	Command string            `yaml:"command" json:"command"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Enabled bool              `yaml:"enabled" json:"enabled"`
}

// MCPTool represents a tool discovered from an MCP server.
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
	ServerName  string          `json:"-"` // Track which server owns this tool
}

// MCPToolCall represents a request to execute a tool.
type MCPToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// MCPToolResult represents the result of a tool execution.
type MCPToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// MCPContent represents content in a tool result.
type MCPContent struct {
	Type     string `json:"type"` // "text" | "image" | "resource"
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// GetTextContent extracts all text content from the result.
func (r *MCPToolResult) GetTextContent() string {
	var result string
	for _, c := range r.Content {
		if c.Type == "text" {
			result += c.Text
		}
	}
	return result
}

// JSON-RPC 2.0 types for MCP protocol communication.

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCP Protocol-specific types

// MCPInitializeParams represents the initialize request parameters.
type MCPInitializeParams struct {
	ProtocolVersion string                `json:"protocolVersion"`
	Capabilities    MCPClientCapabilities `json:"capabilities"`
	ClientInfo      MCPImplementationInfo `json:"clientInfo"`
}

// MCPClientCapabilities represents client capabilities.
type MCPClientCapabilities struct {
	Roots    *MCPRootsCapability `json:"roots,omitempty"`
	Sampling interface{}         `json:"sampling,omitempty"`
}

// MCPRootsCapability represents roots capability.
type MCPRootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// MCPImplementationInfo contains information about the MCP implementation.
type MCPImplementationInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// MCPInitializeResult represents the initialize response result.
type MCPInitializeResult struct {
	ProtocolVersion string                `json:"protocolVersion"`
	Capabilities    MCPServerCapabilities `json:"capabilities"`
	ServerInfo      MCPImplementationInfo `json:"serverInfo"`
	Instructions    string                `json:"instructions,omitempty"`
}

// MCPServerCapabilities represents server capabilities.
type MCPServerCapabilities struct {
	Experimental map[string]interface{}  `json:"experimental,omitempty"`
	Logging      interface{}             `json:"logging,omitempty"`
	Prompts      *MCPPromptsCapability   `json:"prompts,omitempty"`
	Resources    *MCPResourcesCapability `json:"resources,omitempty"`
	Tools        *MCPToolsCapability     `json:"tools,omitempty"`
}

// MCPPromptsCapability represents prompts capability.
type MCPPromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// MCPResourcesCapability represents resources capability.
type MCPResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// MCPToolsCapability represents tools capability.
type MCPToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// MCPToolsListResult represents the tools/list response result.
type MCPToolsListResult struct {
	Tools      []MCPTool `json:"tools"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

// MCPToolsCallParams represents the tools/call request parameters.
type MCPToolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// MCPToolsCallResult represents the tools/call response result.
type MCPToolsCallResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// ToOpenAITool converts an MCP tool to OpenAI tool format.
func (t *MCPTool) ToOpenAITool() Tool {
	return Tool{
		Type: "function",
		Function: Function{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		},
	}
}

// ToOpenAITools converts a slice of MCP tools to OpenAI tools.
func ToOpenAITools(mcpTools []MCPTool) []Tool {
	tools := make([]Tool, len(mcpTools))
	for i, t := range mcpTools {
		tools[i] = t.ToOpenAITool()
	}
	return tools
}
