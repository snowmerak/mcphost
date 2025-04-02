package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/mark3labs/mcphost/pkg/history"
	"github.com/mark3labs/mcphost/pkg/llm"
)

type Runner struct {
	provider   llm.Provider
	mcpClients map[string]*mcpclient.StdioMCPClient
	tools      []llm.Tool

	messages []history.HistoryMessage
}

func NewRunner(provider llm.Provider, mcpClients map[string]*mcpclient.StdioMCPClient, tools []llm.Tool) *Runner {
	return &Runner{
		provider:   provider,
		mcpClients: mcpClients,
		tools:      tools,
		messages:   []history.HistoryMessage{},
	}
}

func (r *Runner) Run(ctx context.Context, prompt string) (string, error) {
	if len(prompt) != 0 {
		r.messages = append(r.messages, history.HistoryMessage{
			Role: "user",
			Content: []history.ContentBlock{{
				Type: "text",
				Text: prompt,
			}},
		})
	}

	llmMessages := make([]llm.Message, len(r.messages))
	for i := range r.messages {
		llmMessages[i] = &r.messages[i]
	}

	const initialBackoff = 1 * time.Second
	const maxRetries int = 5
	const maxBackoff = 30 * time.Second

	var message llm.Message
	var err error
	backoff := initialBackoff
	retries := 0
	for {
		message, err = r.provider.CreateMessage(
			context.Background(),
			prompt,
			llmMessages,
			r.tools,
		)
		if err != nil {
			if strings.Contains(err.Error(), "overloaded_error") {
				if retries >= maxRetries {
					return "", fmt.Errorf(
						"claude is currently overloaded. please wait a few minutes and try again",
					)
				}

				time.Sleep(backoff)
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				retries++
				continue
			}

			return "", err
		}

		break
	}

	var messageContent []history.ContentBlock

	var toolResults []history.ContentBlock
	messageContent = []history.ContentBlock{}

	if message.GetContent() != "" {
		messageContent = append(messageContent, history.ContentBlock{
			Type: "text",
			Text: message.GetContent(),
		})
	}

	for _, toolCall := range message.GetToolCalls() {
		input, _ := json.Marshal(toolCall.GetArguments())
		messageContent = append(messageContent, history.ContentBlock{
			Type:  "tool_use",
			ID:    toolCall.GetID(),
			Name:  toolCall.GetName(),
			Input: input,
		})

		parts := strings.Split(toolCall.GetName(), "__")

		serverName, toolName := parts[0], parts[1]
		mcpClient, ok := r.mcpClients[serverName]
		if !ok {
			continue
		}

		var toolArgs map[string]interface{}
		if err := json.Unmarshal(input, &toolArgs); err != nil {
			continue
		}

		var toolResultPtr *mcp.CallToolResult
		req := mcp.CallToolRequest{}
		req.Params.Name = toolName
		req.Params.Arguments = toolArgs
		toolResultPtr, err = mcpClient.CallTool(
			context.Background(),
			req,
		)

		if err != nil {
			errMsg := fmt.Sprintf(
				"Error calling tool %s: %v",
				toolName,
				err,
			)
			log.Printf("Error calling tool %s: %v", toolName, err)

			toolResults = append(toolResults, history.ContentBlock{
				Type:      "tool_result",
				ToolUseID: toolCall.GetID(),
				Content: []history.ContentBlock{{
					Type: "text",
					Text: errMsg,
				}},
			})

			continue
		}

		toolResult := *toolResultPtr

		if toolResult.Content != nil {
			resultBlock := history.ContentBlock{
				Type:      "tool_result",
				ToolUseID: toolCall.GetID(),
				Content:   toolResult.Content,
			}

			var resultText string
			for _, item := range toolResult.Content {
				if contentMap, ok := item.(map[string]interface{}); ok {
					if text, ok := contentMap["text"]; ok {
						resultText += fmt.Sprintf("%v ", text)
					}
				}
			}

			resultBlock.Text = strings.TrimSpace(resultText)

			toolResults = append(toolResults, resultBlock)
		}
	}

	r.messages = append(r.messages, history.HistoryMessage{
		Role:    message.GetRole(),
		Content: messageContent,
	})

	if len(toolResults) > 0 {
		r.messages = append(r.messages, history.HistoryMessage{
			Role:    "user",
			Content: toolResults,
		})

		return r.Run(ctx, "")
	}

	return message.GetContent(), nil
}
