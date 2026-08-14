package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"google.golang.org/genai"
)

// ============================================================================
// Core Types
// ============================================================================

// MemoryTree stores JSON data
type MemoryTree struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

// MTServer manages multiple memory trees with global lock
type MTServer struct {
	mu      sync.Mutex
	trees   map[string]*MemoryTree
	dataDir string
	hook    OperationHook
}

// OperationHook is called between operations in batch mode
type OperationHook func(operation string, tree string, path string)

// BatchOperation represents a single operation in a batch
type BatchOperation struct {
	Operation  string          `json:"operation"`
	Tree       string          `json:"tree,omitempty"`
	Path       string          `json:"path,omitempty"`
	Value      json.RawMessage `json:"value,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	Filter     string          `json:"filter,omitempty"`
	SourceTree string          `json:"source_tree,omitempty"`
	SourcePath string          `json:"source_path,omitempty"`
	TargetPath string          `json:"target_path,omitempty"`
	Window     int             `json:"window,omitempty"`
	Query      string          `json:"query,omitempty"`
	MaxTokens  int             `json:"max_tokens,omitempty"`
	Verbatim   *bool           `json:"verbatim,omitempty"` // nil = default, true/false = explicit
}

// BatchResult represents the result of a single operation
type BatchResult struct {
	Success   bool   `json:"success"`
	Operation string `json:"operation"`
	Tree      string `json:"tree,omitempty"`
	Path      string `json:"path,omitempty"`
	Index     int    `json:"index"`
	Data      any    `json:"data,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ============================================================================
// Validation and Helpers
// ============================================================================

var safeNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// validateTreeName ensures tree name is safe for filesystem
func validateTreeName(name string) error {
	if !safeNameRegex.MatchString(name) {
		return fmt.Errorf("invalid tree name '%s': must contain only alphanumeric, underscore, or hyphen characters", name)
	}
	return nil
}

// validateJSON ensures the data is valid JSON
func validateJSON(data json.RawMessage) error {
	if data == nil {
		return nil // nil is ok, will be replaced with default
	}
	var temp interface{}
	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// validateWindow ensures window parameter is valid
func validateWindow(window int) error {
	if window < 0 {
		return fmt.Errorf("window must be non-negative, got: %d", window)
	}
	return nil
}

// execJQ executes jq with input and filter
func execJQ(input []byte, filter string) ([]byte, error) {
	if filter == "" {
		return nil, fmt.Errorf("jq filter cannot be empty")
	}

	// Use jq with -s (slurp) to read the filter output and wrap multiple results in an array
	// First pass: run the filter
	// Second pass: slurp the results into an array if there are multiple lines
	cmd := exec.Command("jq", "-c", filter)
	cmd.Stdin = strings.NewReader(string(input))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("jq filter '%s' failed: %s", filter, strings.TrimSpace(string(output)))
	}

	// Check if output has multiple lines (multiple JSON values)
	// If so, wrap them in an array for valid JSON
	trimmed := strings.TrimSpace(string(output))
	if strings.Contains(trimmed, "\n") {
		// Multiple results - use jq -s to slurp them into an array
		cmd2 := exec.Command("jq", "-s", "-c", ".")
		cmd2.Stdin = strings.NewReader(trimmed)
		output2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return nil, fmt.Errorf("jq slurp failed: %s", strings.TrimSpace(string(output2)))
		}
		return output2, nil
	}

	return output, nil
}

// jqSet sets value at path using jq
func jqSet(input []byte, path string, value string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("path cannot be empty for set operation")
	}
	filter := fmt.Sprintf("%s = %s", path, value)
	cmd := exec.Command("jq", "-c", filter)
	cmd.Stdin = strings.NewReader(string(input))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("jq set at path '%s' failed: %s", path, strings.TrimSpace(string(output)))
	}
	return output, nil
}

// getTreePath returns filesystem path for a tree
func (s *MTServer) getTreePath(treeName string) (string, error) {
	if err := validateTreeName(treeName); err != nil {
		return "", err
	}
	return filepath.Join(s.dataDir, "trees", treeName+".json"), nil
}

// shouldIncludeData determines if data should be included in response
func shouldIncludeData(op BatchOperation, isGetOp bool) bool {
	if op.Verbatim != nil {
		return *op.Verbatim
	}
	// Default: get operations include data, others don't
	return isGetOp
}

// ============================================================================
// Persistence
// ============================================================================

// initPersistence creates trees directory and loads existing trees
func (s *MTServer) initPersistence() error {
	treesDir := filepath.Join(s.dataDir, "trees")
	if err := os.MkdirAll(treesDir, 0755); err != nil {
		return fmt.Errorf("failed to create trees directory: %w", err)
	}

	entries, err := os.ReadDir(treesDir)
	if err != nil {
		return fmt.Errorf("failed to read trees directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		treeName := strings.TrimSuffix(entry.Name(), ".json")
		if err := s.loadTree(treeName); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load tree '%s': %v\n", treeName, err)
		}
	}

	return nil
}

// loadTree loads a tree from disk
func (s *MTServer) loadTree(treeName string) error {
	path, err := s.getTreePath(treeName)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read tree file: %w", err)
	}

	if err := validateJSON(data); err != nil {
		return fmt.Errorf("tree file contains invalid JSON: %w", err)
	}

	s.trees[treeName] = &MemoryTree{
		Name: treeName,
		Data: json.RawMessage(data),
	}

	return nil
}

// saveTree atomically saves a tree to disk
func (s *MTServer) saveTree(treeName string) error {
	tree, exists := s.trees[treeName]
	if !exists {
		return fmt.Errorf("tree '%s' not found", treeName)
	}

	path, err := s.getTreePath(treeName)
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, tree.Data, 0644); err != nil {
		return fmt.Errorf("failed to write tree file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename tree file: %w", err)
	}

	return nil
}

// deleteTreeFile removes a tree's file from disk
func (s *MTServer) deleteTreeFile(treeName string) error {
	path, err := s.getTreePath(treeName)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete tree file: %w", err)
	}

	return nil
}

// ============================================================================
// Core Operations (all unlocked, called within global lock)
// ============================================================================

// opCreateTree creates a new tree with optional initial data
func (s *MTServer) opCreateTree(tree string, data json.RawMessage) error {
	if err := validateTreeName(tree); err != nil {
		return err
	}
	if s.trees[tree] != nil {
		return fmt.Errorf("tree '%s' already exists", tree)
	}
	if data == nil {
		data = json.RawMessage("{}")
	}
	if err := validateJSON(data); err != nil {
		return err
	}

	s.trees[tree] = &MemoryTree{Name: tree, Data: data}
	return s.saveTree(tree)
}

// opDeleteTree removes a tree - DISABLED FOR SAFETY
func (s *MTServer) opDeleteTree(tree string) error {
	return fmt.Errorf("delete_tree operation is disabled for safety - trees cannot be deleted")
}

// opListTrees returns metadata about all trees
func (s *MTServer) opListTrees() []map[string]any {
	result := make([]map[string]any, 0, len(s.trees))
	for name, tree := range s.trees {
		result = append(result, map[string]any{
			"name": name,
			"size": len(tree.Data),
		})
	}
	return result
}

// opGet retrieves data at path using jq
func (s *MTServer) opGet(tree, path string) (json.RawMessage, error) {
	t := s.trees[tree]
	if t == nil {
		return nil, fmt.Errorf("tree '%s' not found", tree)
	}
	if path == "" {
		return nil, fmt.Errorf("path parameter is required for get operation")
	}

	result, err := execJQ(t.Data, path)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(result), nil
}

// opSet updates data at path using jq
func (s *MTServer) opSet(tree, path string, value json.RawMessage) error {
	t := s.trees[tree]
	if t == nil {
		return fmt.Errorf("tree '%s' not found", tree)
	}
	if path == "" {
		return fmt.Errorf("path parameter is required for set operation")
	}
	if err := validateJSON(value); err != nil {
		return err
	}

	valueStr := string(value)
	result, err := jqSet(t.Data, path, valueStr)
	if err != nil {
		return err
	}
	t.Data = json.RawMessage(result)
	return s.saveTree(tree)
}

// opDelete removes data at path using jq
func (s *MTServer) opDelete(tree, path string) error {
	t := s.trees[tree]
	if t == nil {
		return fmt.Errorf("tree '%s' not found", tree)
	}
	if path == "" {
		return fmt.Errorf("path parameter is required for delete operation")
	}

	filter := fmt.Sprintf("delpaths([path(%s)])", path)
	result, err := execJQ(t.Data, filter)
	if err != nil {
		return err
	}
	t.Data = json.RawMessage(result)
	return s.saveTree(tree)
}

// opAppend adds items to array at path with optional window limit
func (s *MTServer) opAppend(tree, path string, value json.RawMessage, window int) error {
	t := s.trees[tree]
	if t == nil {
		return fmt.Errorf("tree '%s' not found", tree)
	}
	if path == "" {
		return fmt.Errorf("path parameter is required for append operation")
	}
	if err := validateJSON(value); err != nil {
		return err
	}
	if err := validateWindow(window); err != nil {
		return err
	}

	// Get current array
	current, err := execJQ(t.Data, path)
	if err != nil {
		current = []byte("[]")
	}

	// Parse value as array or single item
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err != nil {
		items = []json.RawMessage{value}
	}

	// Parse current array
	var arr []json.RawMessage
	if err := json.Unmarshal(current, &arr); err != nil {
		arr = []json.RawMessage{}
	}

	// Append items
	arr = append(arr, items...)

	// Apply window limit (keep last N)
	if window > 0 && len(arr) > window {
		arr = arr[len(arr)-window:]
	}

	// Set back
	newArr, _ := json.Marshal(arr)
	return s.opSet(tree, path, json.RawMessage(newArr))
}

// opPrepend adds items to beginning of array at path with optional window limit
func (s *MTServer) opPrepend(tree, path string, value json.RawMessage, window int) error {
	t := s.trees[tree]
	if t == nil {
		return fmt.Errorf("tree '%s' not found", tree)
	}
	if path == "" {
		return fmt.Errorf("path parameter is required for prepend operation")
	}
	if err := validateJSON(value); err != nil {
		return err
	}
	if err := validateWindow(window); err != nil {
		return err
	}

	// Get current array
	current, err := execJQ(t.Data, path)
	if err != nil {
		current = []byte("[]")
	}

	// Parse value as array or single item
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err != nil {
		items = []json.RawMessage{value}
	}

	// Parse current array
	var arr []json.RawMessage
	if err := json.Unmarshal(current, &arr); err != nil {
		arr = []json.RawMessage{}
	}

	// Prepend items
	arr = append(items, arr...)

	// Apply window limit (keep first N)
	if window > 0 && len(arr) > window {
		arr = arr[:window]
	}

	// Set back
	newArr, _ := json.Marshal(arr)
	return s.opSet(tree, path, json.RawMessage(newArr))
}

// opTransform applies jq filter to source and stores at target (supports cross-tree)
func (s *MTServer) opTransform(sourceTree, sourcePath, targetTree, targetPath, filter string) error {
	// Validate source tree
	srcTree := s.trees[sourceTree]
	if srcTree == nil {
		return fmt.Errorf("source tree '%s' not found", sourceTree)
	}

	// Validate target tree
	tgtTree := s.trees[targetTree]
	if tgtTree == nil {
		return fmt.Errorf("target tree '%s' not found", targetTree)
	}

	if sourcePath == "" {
		return fmt.Errorf("source_path parameter is required for transform operation")
	}
	if targetPath == "" {
		return fmt.Errorf("target_path parameter is required for transform operation")
	}
	if filter == "" {
		return fmt.Errorf("filter parameter is required for transform operation")
	}

	// Get source data
	sourceData, err := execJQ(srcTree.Data, sourcePath)
	if err != nil {
		return fmt.Errorf("failed to get source data: %w", err)
	}

	// Apply transformation
	result, err := execJQ(sourceData, filter)
	if err != nil {
		return fmt.Errorf("transformation failed: %w", err)
	}

	// Set at target
	return s.opSet(targetTree, targetPath, json.RawMessage(result))
}

// opQuery executes jq filter on tree and returns result
func (s *MTServer) opQuery(tree, filter string) (json.RawMessage, error) {
	t := s.trees[tree]
	if t == nil {
		return nil, fmt.Errorf("tree '%s' not found", tree)
	}
	if filter == "" {
		return nil, fmt.Errorf("filter parameter is required for query operation")
	}

	result, err := execJQ(t.Data, filter)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(result), nil
}

// opGeminiSearch performs a Google search using Gemini AI
func (s *MTServer) opGeminiSearch(query string, maxTokens int) (any, error) {
	if query == "" {
		return nil, fmt.Errorf("query parameter is required for gemini_search operation")
	}
	if maxTokens <= 0 {
		maxTokens = 1000
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable not set")
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	response, err := client.Models.GenerateContent(ctx, "gemini-2.5-pro", []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				{Text: fmt.Sprintf("Use the GoogleSearch tool to search for: %s", query)},
			},
		},
	}, &genai.GenerateContentConfig{
		MaxOutputTokens: int32(maxTokens),
		Tools:           []*genai.Tool{{GoogleSearch: &genai.GoogleSearch{}}},
	})

	if err != nil {
		return nil, fmt.Errorf("Gemini search failed: %w", err)
	}
	return response, nil
}

// ============================================================================
// Tool Schema and Handler
// ============================================================================

// createMTTool defines the unified mt tool schema (batch-only mode)
func createMTTool() mcp.Tool {
	return mcp.Tool{
		Name:        "mt",
		Description: "Unified memory tree operations. Supports batch arrays. Each operation in a batch operates on the state produced by the previous operation, allowing multi-step data processing.",
		InputSchema: mcp.ToolInputSchema{
			Type:     "object",
			Required: []string{"operations"},
			Properties: map[string]any{
				"operations": map[string]any{
					"type":        "array",
					"description": "Array of operations to perform in sequence. Each operation sees the state produced by the previous operation.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"operation": map[string]any{
								"type":        "string",
								"enum":        []string{"create_tree", "list_trees", "get", "set", "append", "prepend", "transform", "query", "gemini_search"},
								"description": "Operation to perform",
							},
							"tree": map[string]any{
								"type":        "string",
								"description": "Tree name",
							},
							"path": map[string]any{
								"type":        "string",
								"description": "jq path expression (REQUIRED - no default, must be explicit)",
							},
							"value": map[string]any{
								"description": "Value for operation (JSON)",
							},
							"data": map[string]any{
								"description": "Initial data for create_tree (JSON)",
							},
							"filter": map[string]any{
								"type":        "string",
								"description": "jq filter expression for query/transform operations",
							},
							"source_tree": map[string]any{
								"type":        "string",
								"description": "Source tree name for transform operation (defaults to same as tree parameter)",
							},
							"source_path": map[string]any{
								"type":        "string",
								"description": "Source path for transform operation (REQUIRED for transform)",
							},
							"target_path": map[string]any{
								"type":        "string",
								"description": "Target path for transform operation (REQUIRED for transform)",
							},
							"window": map[string]any{
								"type":        "integer",
								"description": "Maximum array length for append/prepend operations (0 = no limit)",
							},
							"query": map[string]any{
								"type":        "string",
								"description": "Search query for gemini_search operation",
							},
							"max_tokens": map[string]any{
								"type":        "integer",
								"description": "Maximum tokens for gemini_search operation (default: 1000)",
							},
							"verbatim": map[string]any{
								"type":        "boolean",
								"description": "Include tree data in response (default: true for get, false for others)",
							},
						},
						"required": []string{"operation"},
					},
				},
			},
		},
	}
}

// handleMT processes mt tool operations with global lock (batch-only mode)
func (s *MTServer) handleMT(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Global lock for entire operation
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.handleBatchMT(ctx, request)
}

// handleBatchMT processes batch operations
func (s *MTServer) handleBatchMT(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	operationsArray := request.GetArguments()["operations"]

	var operationsSlice []interface{}
	switch v := operationsArray.(type) {
	case []interface{}:
		operationsSlice = v
	case []map[string]any:
		operationsSlice = make([]interface{}, len(v))
		for i, op := range v {
			operationsSlice[i] = op
		}
	default:
		return mcp.NewToolResultError(fmt.Sprintf("operations must be an array, got %T", operationsArray)), nil
	}

	// Parse operations
	operations := make([]BatchOperation, len(operationsSlice))
	for i, opInterface := range operationsSlice {
		opBytes, err := json.Marshal(opInterface)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid operation at index %d: %v", i, err)), nil
		}
		if err := json.Unmarshal(opBytes, &operations[i]); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid operation at index %d: %v", i, err)), nil
		}
	}

	// Execute batch
	results := make([]BatchResult, len(operations))
	for i, op := range operations {
		results[i] = s.executeSingleOp(op, i)

		// Call hook between operations
		if s.hook != nil && results[i].Success {
			s.hook(op.Operation, op.Tree, op.Path)
		}

		// Stop on first error (sequential dependency model)
		if !results[i].Success {
			// Mark remaining as skipped
			for j := i + 1; j < len(results); j++ {
				results[j] = BatchResult{
					Success:   false,
					Operation: operations[j].Operation,
					Tree:      operations[j].Tree,
					Index:     j,
					Error:     "skipped due to previous operation failure",
				}
			}
			break
		}
	}

	// Build response
	successCount := 0
	errorCount := 0
	verbatimResults := make([]BatchResult, 0)

	for i, result := range results {
		if result.Success {
			successCount++
		} else {
			errorCount++
		}

		// Always include error results, or include success results if verbatim=true
		isReadOp := operations[i].Operation == "get" || operations[i].Operation == "query" || operations[i].Operation == "gemini_search" || operations[i].Operation == "list_trees"
		if !result.Success || shouldIncludeData(operations[i], isReadOp) {
			verbatimResults = append(verbatimResults, result)
		}
	}

	response := map[string]any{
		"success":               errorCount == 0,
		"total_operations":      len(operations),
		"successful_operations": successCount,
		"failed_operations":     errorCount,
	}

	if len(verbatimResults) > 0 {
		response["results"] = verbatimResults
	}

	return mcp.NewToolResultStructuredOnly(response), nil
}

// executeSingleOp executes a single operation (called within lock)
func (s *MTServer) executeSingleOp(op BatchOperation, index int) BatchResult {
	result := BatchResult{
		Operation: op.Operation,
		Tree:      op.Tree,
		Path:      op.Path,
		Index:     index,
	}

	var err error
	var data any

	switch op.Operation {
	case "create_tree":
		if op.Tree == "" {
			result.Error = "tree parameter is required for create_tree operation"
			return result
		}
		err = s.opCreateTree(op.Tree, op.Data)

	// delete_tree operation DISABLED FOR SAFETY
	// case "delete_tree":
	// 	if op.Tree == "" {
	// 		result.Error = "tree parameter is required for delete_tree operation"
	// 		return result
	// 	}
	// 	err = s.opDeleteTree(op.Tree)

	case "list_trees":
		data = s.opListTrees()

	case "get":
		if op.Tree == "" {
			result.Error = "tree parameter is required for get operation"
			return result
		}
		data, err = s.opGet(op.Tree, op.Path)

	case "set":
		if op.Tree == "" {
			result.Error = "tree parameter is required for set operation"
			return result
		}
		if op.Value == nil {
			result.Error = "value parameter is required for set operation"
			return result
		}
		err = s.opSet(op.Tree, op.Path, op.Value)

	// delete operation DISABLED FOR SAFETY
	// case "delete":
	// 	if op.Tree == "" {
	// 		result.Error = "tree parameter is required for delete operation"
	// 		return result
	// 	}
	// 	err = s.opDelete(op.Tree, op.Path)

	case "append":
		if op.Tree == "" {
			result.Error = "tree parameter is required for append operation"
			return result
		}
		if op.Value == nil {
			result.Error = "value parameter is required for append operation"
			return result
		}
		err = s.opAppend(op.Tree, op.Path, op.Value, op.Window)

	case "prepend":
		if op.Tree == "" {
			result.Error = "tree parameter is required for prepend operation"
			return result
		}
		if op.Value == nil {
			result.Error = "value parameter is required for prepend operation"
			return result
		}
		err = s.opPrepend(op.Tree, op.Path, op.Value, op.Window)

	case "transform":
		sourceTree := op.SourceTree
		if sourceTree == "" {
			sourceTree = op.Tree
		}
		if sourceTree == "" {
			result.Error = "tree or source_tree parameter is required for transform operation"
			return result
		}
		targetTree := op.Tree
		if targetTree == "" {
			result.Error = "tree parameter is required for transform operation (target tree)"
			return result
		}
		err = s.opTransform(sourceTree, op.SourcePath, targetTree, op.TargetPath, op.Filter)

	case "query":
		if op.Tree == "" {
			result.Error = "tree parameter is required for query operation"
			return result
		}
		data, err = s.opQuery(op.Tree, op.Filter)

	case "gemini_search":
		data, err = s.opGeminiSearch(op.Query, op.MaxTokens)

	default:
		result.Error = fmt.Sprintf("unknown operation: %s", op.Operation)
		return result
	}

	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true

	// Include data if verbatim or is read operation
	isReadOp := op.Operation == "get" || op.Operation == "query" || op.Operation == "gemini_search" || op.Operation == "list_trees"
	if shouldIncludeData(op, isReadOp) {
		result.Data = data
	}

	return result
}

// ============================================================================
// Server Setup
// ============================================================================

// NewMTServer creates a new memory tree server
func NewMTServer(dataDir string) (*MTServer, error) {
	s := &MTServer{
		trees:   make(map[string]*MemoryTree),
		dataDir: dataDir,
	}

	if err := s.initPersistence(); err != nil {
		return nil, fmt.Errorf("failed to initialize persistence: %w", err)
	}

	return s, nil
}

// SetHook sets the operation hook for batch processing
func (s *MTServer) SetHook(hook OperationHook) {
	s.hook = hook
}

// ============================================================================
// Main
// ============================================================================

func main() {
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

	mtServer, err := NewMTServer(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create server: %v\n", err)
		os.Exit(1)
	}

	// Setup graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "Shutting down gracefully...")
		cancel()
	}()

	// Create MCP server
	mcpServer := server.NewMCPServer(
		"mt-server",
		"2.0.0",
		server.WithInstructions("Memory Tree server for managing JSON tree structures with jq-powered operations. All operations are immediate persistence. Operations in batch mode are sequential - each sees the state produced by the previous operation."),
		server.WithToolCapabilities(false),
	)

	// Register the mt tool
	mcpServer.AddTool(createMTTool(), mtServer.handleMT)

	// Run STDIO server
	stdioServer := server.NewStdioServer(mcpServer)
	if err := stdioServer.Listen(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
