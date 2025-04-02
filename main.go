package main

import (
	"context"
	"flag"
	"os"
	"os/signal"

	"github.com/charmbracelet/log"

	"github.com/mark3labs/mcphost/internal/runner"
	"github.com/mark3labs/mcphost/pkg/llm/ollama"
)

var (
	model  = flag.String("model", "qwen2.5-coder:14b", "Model name")
	config = flag.String("config", ".mcp.json", "Path to MCP config file")
)

func main() {
	flag.Parse()

	provider, err := ollama.NewProvider(*model)
	if err != nil {
		panic(err)
	}

	clients, tools, err := runner.LoadMCPClients(*config)
	if err != nil {
		panic(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	rn := runner.NewRunner(provider, clients, tools)

	result, err := rn.Run(ctx, "가장 최근 커밋 메시지가 뭐야?")
	if err != nil {
		panic(err)
	}

	log.Printf("Result: %s", result)
}
