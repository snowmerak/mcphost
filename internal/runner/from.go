package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/mark3labs/mcphost/pkg/llm"
)

func Recover(f func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered from panic: %v", r)
		}
	}()

	f()
}

var (
	aliveMCPClientCount = atomic.Int64{}
)

func LoadAliveMCPClientCount() int64 {
	return aliveMCPClientCount.Load()
}

func LoadMCPClients(ctx context.Context, configPath string) (map[string]*mcpclient.StdioMCPClient, []llm.Tool, error) {
	f, err := os.Open(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer f.Close()

	var config MCPConfig
	decoder := json.NewDecoder(f)
	if err := decoder.Decode(&config); err != nil {
		return nil, nil, fmt.Errorf("failed to decode config file: %w", err)
	}

	if len(config.MCPServers) == 0 {
		return nil, nil, fmt.Errorf("no MCP servers found in config file")
	}

	clients, err := createMCPClients(ctx, &config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create MCP clients: %w", err)
	}

	tools := make([]llm.Tool, 0, len(clients))
	for name, client := range clients {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		tool, err := client.ListTools(ctx, mcp.ListToolsRequest{})
		cancel()
		if err != nil {
			Recover(func() {
				client.Close()
			})
			for _, c := range clients {
				Recover(func() {
					c.Close()
				})
			}
			return nil, nil, fmt.Errorf("failed to list tools for %s: %w", name, err)
		}

		serverTools := mcpToolsToAnthropicTools(name, tool.Tools)
		tools = append(tools, serverTools...)
	}

	return clients, tools, nil
}

type MCPConfig struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

type ServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

func createMCPClients(
	ctx context.Context,
	config *MCPConfig,
) (map[string]*mcpclient.StdioMCPClient, error) {
	clients := make(map[string]*mcpclient.StdioMCPClient)

	for name, server := range config.MCPServers {
		var env []string
		for k, v := range server.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		client, err := mcpclient.NewStdioMCPClient(
			server.Command,
			env,
			server.Args...)
		if err != nil {
			for _, c := range clients {
				Recover(func() {
					c.Close()
				})
			}
			return nil, fmt.Errorf(
				"failed to create MCP client for %s: %w",
				name,
				err,
			)
		}

		if err := func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			initRequest := mcp.InitializeRequest{}
			initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
			initRequest.Params.ClientInfo = mcp.Implementation{
				Name:    "mcphost",
				Version: "0.1.0",
			}
			initRequest.Params.Capabilities = mcp.ClientCapabilities{}

			_, err = client.Initialize(ctx, initRequest)
			if err != nil {
				Recover(func() {
					client.Close()
				})
				for _, c := range clients {
					Recover(func() {
						c.Close()
					})
				}
				return fmt.Errorf(
					"failed to initialize MCP client for %s: %w",
					name,
					err,
				)
			}

			return nil
		}(); err != nil {
			return nil, err
		}

		clients[name] = client

		aliveMCPClientCount.Add(1)
		context.AfterFunc(ctx, func() {
			Recover(func() {
				client.Close()
			})
			aliveMCPClientCount.Add(-1)
		})
	}

	return clients, nil
}

func mcpToolsToAnthropicTools(
	serverName string,
	mcpTools []mcp.Tool,
) []llm.Tool {
	anthropicTools := make([]llm.Tool, len(mcpTools))

	for i, tool := range mcpTools {
		namespacedName := fmt.Sprintf("%s__%s", serverName, tool.Name)

		anthropicTools[i] = llm.Tool{
			Name:        namespacedName,
			Description: tool.Description,
			InputSchema: llm.Schema{
				Type:       tool.InputSchema.Type,
				Properties: tool.InputSchema.Properties,
				Required:   tool.InputSchema.Required,
			},
		}
	}

	return anthropicTools
}
