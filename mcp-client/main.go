package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	openai "github.com/sashabaranov/go-openai"
)

// Message structure for conversation
type Message struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"` // text content
}

func main() {
	// Get OpenAI API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable not set")
	}

	// MCP server SSE URL
	mcpURL := "http://localhost:8081/sse"
	if u := os.Getenv("MCP_SERVER_URL"); u != "" {
		mcpURL = u
	}

	// Connect to MCP server
	mcpClient, err := client.NewSSEMCPClient(mcpURL)
	if err != nil {
		log.Fatalf("failed to connect to MCP server: %v", err)
	}
	defer mcpClient.Close()

	ctx := context.Background()

	// Start client connection before Initialize
	ready := make(chan error, 1)

	go func() {
		// Start blocks until error or close
		ready <- mcpClient.Start(ctx)
	}()

	// Wait for transport to be ready
	if err := <-ready; err != nil {
		log.Fatalf("mcp connection start failed: %v", err)
	}

	// Initialize MCP session
	initResp, err := mcpClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			ClientInfo: mcp.Implementation{
				Name:    "jenkins-llm-bridge",
				Version: "0.1",
			},
		},
	})
	if err != nil {
		log.Fatalf("failed to initialize MCP client: %v", err)
	}

	fmt.Printf("MCP initialized. Server: %+v\n", initResp.ServerInfo)

	// OpenAI client
	oa := openai.NewClient(apiKey)

	// Conversation history
	history := []Message{
		{
			Role: "system",
			Content: `You are a DevOps assistant. 
					- When the user asks to run, start, or trigger a Jenkins job, respond ONLY with:
					TOOL: trigger_job {"job_name": "<name>"}
					- When the user asks for build status, respond ONLY with:
					TOOL: get_build_status {"job_name": "<name>", "build_number": <number>}
					- When the user asks for console logs, respond ONLY with:
					TOOL: get_console_log {"job_name": "<name>", "build_number": <number>}

					Never answer in natural language for these cases.`,
		},
	}

	fmt.Println("Jenkins LLM Bridge started. Type your prompts:")

	// REPL loop
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		var input string
		if !scanner.Scan() {
			break
		}
		input = scanner.Text()

		if err != nil {
			if err.Error() == "unexpected newline" {
				continue
			}
			log.Printf("input error: %v", err)
			continue
		}

		// Append user message
		history = append(history, Message{
			Role:    "user",
			Content: input,
		})

		// Send prompt to OpenAI
		ctx := context.Background()
		resp, err := oa.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:       "gpt-4o-mini",
			Messages:    convertMessages(history),
			Temperature: 0.2,
		})
		if err != nil {
			log.Printf("OpenAI error: %v", err)
			continue
		}

		llmReply := resp.Choices[0].Message.Content
		fmt.Printf("LLM: %s\n", llmReply)

		// Check if LLM wants to call a tool
		if toolCall := parseToolCall(llmReply); toolCall != nil {
			fmt.Printf("→ Detected MCP tool call: %+v\n", toolCall)

			// Convert to CallToolRequest
			req := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      toolCall.Name,
					Arguments: toolCall.Params,
				},
			}

			// Call MCP tool
			toolResp, err := mcpClient.CallTool(ctx, req)
			if err != nil {
				fmt.Printf("MCP call error: %v\n", err)
				continue
			}

			fmt.Printf("→ Tool result: %+v\n", toolResp)

			// Append assistant message including tool output
			history = append(history, Message{
				Role:    "assistant",
				Content: fmt.Sprintf("[Tool output]: %v", toolResp.Result),
			})
		} else {
			// Normal LLM reply
			history = append(history, Message{
				Role:    "assistant",
				Content: llmReply,
			})
		}
	}
}

// convertMessages maps history to OpenAI chat messages
func convertMessages(history []Message) []openai.ChatCompletionMessage {
	m := []openai.ChatCompletionMessage{}
	for _, msg := range history {
		role := msg.Role // keep "system", "user", "assistant"
		m = append(m, openai.ChatCompletionMessage{
			Role:    role,
			Content: msg.Content,
		})
	}
	return m
}

// ToolCall struct
type ToolCall struct {
	Name   string
	Params map[string]any
}

// parseToolCall parses LLM output for a simple "call tool" syntax
// Example expected format: TOOL: trigger_job {"job_name":"demo-job"}
func parseToolCall(reply string) *ToolCall {
	reply = strings.TrimSpace(reply)
	if !strings.HasPrefix(reply, "TOOL:") {
		return nil
	}

	// Split into tool name and the rest
	raw := strings.TrimSpace(reply[len("TOOL:"):])
	parts := strings.SplitN(raw, " ", 2)
	if len(parts) < 2 {
		return nil
	}

	name := strings.TrimSpace(parts[0])
	rawParams := strings.TrimSpace(parts[1])

	// Extract only the {...} part for JSON safety
	start := strings.Index(rawParams, "{")
	end := strings.LastIndex(rawParams, "}")
	if start == -1 || end == -1 || end <= start {
		log.Printf("invalid tool params, no JSON object found: %s", rawParams)
		return nil
	}
	jsonStr := rawParams[start : end+1]

	// Fix common LLM issues: True/False/None → true/false/null
	jsonStr = strings.ReplaceAll(jsonStr, "True", "true")
	jsonStr = strings.ReplaceAll(jsonStr, "False", "false")
	jsonStr = strings.ReplaceAll(jsonStr, "None", "null")

	// Parse JSON
	var params map[string]any
	err := json.Unmarshal([]byte(jsonStr), &params)
	if err != nil {
		log.Printf("failed to parse tool params JSON: %v\nraw JSON: %s", err, jsonStr)
		return nil
	}

	return &ToolCall{Name: name, Params: params}
}
