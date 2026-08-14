package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"
)

// MemoryTree stores JSON data with metadata
type MemoryTree struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

// MTServer manages multiple memory trees
type MTServer struct {
	mu         sync.RWMutex
	trees      map[string]*MemoryTree
	dataDir    string
	saveMode   string // "immediate" or "periodic"
	dirtyTrees map[string]bool
	saveTimer  *time.Timer
	saveTicker *time.Ticker
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

// NewMTServer creates a new memory tree server
func NewMTServer(dataDir, saveMode string) (*MTServer, error) {
	if saveMode == "" {
		saveMode = "periodic"
	}
	if saveMode != "immediate" && saveMode != "periodic" {
		return nil, fmt.Errorf("invalid save mode: %s (must be 'immediate' or 'periodic')", saveMode)
	}

	s := &MTServer{
		trees:      make(map[string]*MemoryTree),
		dataDir:    dataDir,
		saveMode:   saveMode,
		dirtyTrees: make(map[string]bool),
		shutdownCh: make(chan struct{}),
	}

	// Initialize persistence
	if err := s.initPersistence(); err != nil {
		return nil, fmt.Errorf("failed to initialize persistence: %w", err)
	}

	// Start periodic save goroutine if in periodic mode
	if saveMode == "periodic" {
		s.saveTimer = time.NewTimer(5 * time.Second)
		s.saveTimer.Stop() // Don't start until first write
		s.saveTicker = time.NewTicker(30 * time.Second)
		s.wg.Add(1)
		go s.periodicSaveLoop()
	}

	return s, nil
}

// Shutdown gracefully shuts down the server, flushing any pending writes
func (s *MTServer) Shutdown() error {
	close(s.shutdownCh)
	s.wg.Wait()

	// Final save of any dirty trees
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveDirtyTreesLocked()
}

func main() {
	// Command-line flags
	dataDirFlag := flag.String("data-dir", "", "Directory for storing tree data (overridden by DATA_DIR environment variable if set)")
	flag.Parse()

	// Determine data directory: env var takes precedence, then flag, then default
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		if *dataDirFlag != "" {
			dataDir = *dataDirFlag
		} else {
			dataDir = "./data"
		}
	}

	saveMode := os.Getenv("SAVE_MODE")
	if saveMode == "" {
		saveMode = "periodic"
	}

	mtServer, err := NewMTServer(dataDir, saveMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create server: %v\n", err)
		os.Exit(1)
	}

	// Setup graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signal in background
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "Shutting down gracefully...")
		if err := mtServer.Shutdown(); err != nil {
			fmt.Fprintf(os.Stderr, "Shutdown error: %v\n", err)
		}
		cancel()
	}()

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
	if err := stdioServer.Listen(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		mtServer.Shutdown()
		os.Exit(1)
	}

	// Ensure clean shutdown
	mtServer.Shutdown()
}
