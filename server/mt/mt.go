package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/mark3labs/mcp-go/server"
)

// MemoryTree stores JSON data with metadata
type MemoryTree struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

// MTServer manages multiple memory trees
type MTServer struct {
	mu    sync.RWMutex
	trees map[string]*MemoryTree
}

// NewMTServer creates a new memory tree server
func NewMTServer() *MTServer {
	return &MTServer{trees: make(map[string]*MemoryTree)}
}

func main() {
	mtServer := NewMTServer()

	// Create MCP server
	mcpServer := server.NewMCPServer(
		"mt-server",
		"1.0.0",
		server.WithInstructions("Memory Tree server for managing JSON tree structures with jq-powered operations."),
		server.WithToolCapabilities(false),
	)

	// Register the mt tool
	mcpServer.AddTool(createMTTool(), mtServer.handleMT)

	// Run STDIO server
	stdioServer := server.NewStdioServer(mcpServer)
	if err := stdioServer.Listen(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
