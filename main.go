package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"time"

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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	provider, err := ollama.NewProvider(*model)
	if err != nil {
		panic(err)
	}

	clients, tools, err := runner.LoadMCPClients(ctx, *config)
	if err != nil {
		panic(err)
	}

	rn := runner.NewRunner(provider, clients, tools)

	result, err := rn.Run(ctx, "가위 바위 보를 하자. 난 가위야.")
	if err != nil {
		panic(err)
	}

	log.Printf("Result: %s", result)

	<-ctx.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ac := runner.LoadAliveMCPClientCount()
		log.Printf("waiting for %d clients to finish", ac)

		if ac == 0 {
			log.Info("all clients finished")
			break
		}
	}
}
