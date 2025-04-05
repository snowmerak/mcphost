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
	model  = flag.String("model", "mistral-small", "Model name")
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

	rn := runner.NewRunner(runner.SystemPrompt, provider, clients, tools)

	result, err := rn.Run(ctx, "데이터베이스에 유저 데이터를 저장하는 테이블을 생성하고, 더미 데이터를 추가해줘.")
	if err != nil {
		panic(err)
	}

	log.Printf("Result: %s", result)

	<-ctx.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ac := runner.LoadAliveMCPClientCount()
		log.Printf("waiting for %d clients to finish", ac)

		if ac == 0 {
			log.Info("all clients finished")
			break
		}

		ticker.Reset(5 * time.Second)
	}
}
