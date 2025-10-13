package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

const MaxBatchOperations = 32

// BatchOperation represents a single operation in a batch
type BatchOperation struct {
	Operation  string          `json:"operation"`
	Tree       string          `json:"tree,omitempty"`
	Path       string          `json:"path,omitempty"`
	Value      json.RawMessage `json:"value,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	Filter     string          `json:"filter,omitempty"`
	SourcePath string          `json:"source_path,omitempty"`
	Window     int             `json:"window,omitempty"`
	Query      string          `json:"query,omitempty"`
	MaxTokens  int             `json:"max_tokens,omitempty"`
	Verbatim   bool            `json:"verbatim,omitempty"` // Default false - don't echo result back
}

// BatchResult represents the result of a single operation in a batch
type BatchResult struct {
	Success   bool   `json:"success"`
	Operation string `json:"operation"`
	Tree      string `json:"tree,omitempty"`
	Path      string `json:"path,omitempty"`
	Index     int    `json:"index"`
	Data      any    `json:"data,omitempty"`
	Error     string `json:"error,omitempty"`
}

// BatchRequest represents the full batch request
type BatchRequest struct {
	Operations          []BatchOperation `json:"operations"`
	ContinueAfterErrors bool             `json:"continue_after_errors"` // Default false
}

// handleBatchMT processes batch operations
func (s *MTServer) handleBatchMT(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	operationsArray := request.GetArguments()["operations"]
	continueAfterErrors := request.GetBool("continue_after_errors", false)

	// Try different types that could be passed
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

	if len(operationsSlice) > MaxBatchOperations {
		return mcp.NewToolResultError(fmt.Sprintf("maximum %d operations allowed per batch", MaxBatchOperations)), nil
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

	// Start transaction
	txn := s.beginTransaction()
	defer s.commitTransaction(txn)

	// Execute batch
	results := s.executeBatch(ctx, operations, continueAfterErrors, txn)

	// Build response - only include results where verbatim=true
	verbatimResults := make([]BatchResult, 0)
	successCount := 0
	errorCount := 0

	for _, result := range results {
		if result.Success {
			successCount++
		} else {
			errorCount++
		}

		// Find corresponding operation to check verbatim flag
		if result.Index < len(operations) && operations[result.Index].Verbatim {
			verbatimResults = append(verbatimResults, result)
		}
	}

	response := map[string]any{
		"success":               errorCount == 0,
		"total_operations":      len(operations),
		"successful_operations": successCount,
		"failed_operations":     errorCount,
	}

	// Only include results if any operations had verbatim=true
	if len(verbatimResults) > 0 {
		response["batch_results"] = verbatimResults
	}

	return mcp.NewToolResultStructuredOnly(response), nil
}

// executeBatch executes multiple operations with optimized locking
func (s *MTServer) executeBatch(ctx context.Context, operations []BatchOperation, continueAfterErrors bool, txn *Transaction) []BatchResult {
	results := make([]BatchResult, len(operations))

	// Group operations by tree for optimized locking
	treeOps := make(map[string][]int)
	globalOps := make([]int, 0) // Operations that don't operate on specific trees

	for i, op := range operations {
		if op.Tree != "" {
			treeOps[op.Tree] = append(treeOps[op.Tree], i)
		} else {
			globalOps = append(globalOps, i)
		}
	}

	// Execute global operations first
	for _, idx := range globalOps {
		result := s.executeSingleBatchOp(ctx, operations[idx], idx, txn)
		results[idx] = result

		if !result.Success && !continueAfterErrors {
			// Fill remaining results with "skipped" status
			for j := idx + 1; j < len(results); j++ {
				results[j] = BatchResult{
					Success:   false,
					Operation: operations[j].Operation,
					Tree:      operations[j].Tree,
					Index:     j,
					Error:     "skipped due to previous error",
				}
			}
			return results
		}
	}

	// Execute tree operations grouped by tree to minimize lock contention
	for treeName, indices := range treeOps {
		s.mu.Lock()

		hasChanges := false
		for _, idx := range indices {
			result := s.executeSingleBatchOpLocked(ctx, operations[idx], idx, txn)
			results[idx] = result

			if result.Success && isModifyingOperation(operations[idx].Operation) {
				hasChanges = true
			}

			if !result.Success && !continueAfterErrors {
				s.mu.Unlock()
				// Fill remaining results with "skipped" status
				for j := idx + 1; j < len(results); j++ {
					results[j] = BatchResult{
						Success:   false,
						Operation: operations[j].Operation,
						Tree:      operations[j].Tree,
						Index:     j,
						Error:     "skipped due to previous error",
					}
				}
				return results
			}
		}

		// Batch persistence for the tree if there were changes
		if hasChanges {
			if s.saveMode == "immediate" {
				s.saveTree(treeName)
			} else {
				s.markDirty(treeName)
			}
		}

		s.mu.Unlock()
	}

	return results
}

// executeSingleBatchOp executes a single operation in a batch (for global operations)
func (s *MTServer) executeSingleBatchOp(ctx context.Context, op BatchOperation, index int, txn *Transaction) BatchResult {
	result := BatchResult{
		Operation: op.Operation,
		Tree:      op.Tree,
		Path:      op.Path,
		Index:     index,
	}

	switch op.Operation {
	case "list_trees":
		// Use unlocked version to avoid deadlock
		s.mu.RLock()
		treeList := make([]map[string]any, 0, len(s.trees))
		for name := range s.trees {
			treeList = append(treeList, map[string]any{
				"name": name,
				"size": len(s.trees[name].Data),
			})
		}
		s.mu.RUnlock()

		result.Success = true
		if op.Verbatim {
			result.Data = treeList
		}
		return result

	default:
		result.Error = fmt.Sprintf("operation %s requires tree parameter or is not supported in batch", op.Operation)
		return result
	}
}

// executeSingleBatchOpLocked executes a single operation with lock already held
func (s *MTServer) executeSingleBatchOpLocked(ctx context.Context, op BatchOperation, index int, txn *Transaction) BatchResult {
	result := BatchResult{
		Operation: op.Operation,
		Tree:      op.Tree,
		Path:      op.Path,
		Index:     index,
	}

	if op.Tree == "" {
		result.Error = "tree parameter required for this operation"
		return result
	}

	switch op.Operation {
	case "create_tree":
		// Create tree without additional locking since we already hold the lock
		if s.trees[op.Tree] != nil {
			result.Error = fmt.Sprintf("tree %s already exists", op.Tree)
			return result
		}

		tree := &MemoryTree{
			Name: op.Tree,
			Data: op.Data,
		}
		if tree.Data == nil {
			tree.Data = json.RawMessage("{}")
		}

		s.trees[op.Tree] = tree
		result.Success = true
		s.recordChange(txn, "create_tree", op.Tree, "")
		return result

	case "delete_tree":
		if s.trees[op.Tree] == nil {
			result.Error = fmt.Sprintf("tree %s not found", op.Tree)
			return result
		}
		delete(s.trees, op.Tree)
		result.Success = true
		s.recordChange(txn, "delete_tree", op.Tree, "")
		return result

	case "get":
		path := op.Path
		if path == "" {
			path = "."
		}
		data, err := s.getUnlocked(op.Tree, path)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Success = true
		if op.Verbatim {
			var parsed any
			json.Unmarshal(data, &parsed)
			result.Data = parsed
		}
		return result

	case "set":
		path := op.Path
		if path == "" {
			path = "."
		}
		err := s.setUnlocked(op.Tree, path, op.Value)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Success = true
		s.recordChange(txn, "set", op.Tree, path)
		return result

	case "delete":
		path := op.Path
		if path == "" {
			path = "."
		}
		err := s.deleteUnlocked(op.Tree, path)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Success = true
		s.recordChange(txn, "delete", op.Tree, path)
		return result

	case "append":
		path := op.Path
		if path == "" {
			path = "."
		}
		err := s.append(op.Tree, path, op.Value, op.Window)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Success = true
		s.recordChange(txn, "append", op.Tree, path)
		return result

	case "prepend":
		path := op.Path
		if path == "" {
			path = "."
		}
		err := s.prepend(op.Tree, path, op.Value, op.Window)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Success = true
		s.recordChange(txn, "prepend", op.Tree, path)
		return result

	case "transform":
		sourcePath := op.SourcePath
		if sourcePath == "" {
			sourcePath = "."
		}
		targetPath := op.Path
		if targetPath == "" {
			targetPath = "."
		}
		err := s.transform(op.Tree, targetPath, sourcePath, op.Filter)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Success = true
		s.recordChange(txn, "transform", op.Tree, targetPath)
		return result

	case "query":
		data, err := s.query(op.Tree, op.Filter)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Success = true
		if op.Verbatim {
			var parsed any
			json.Unmarshal(data, &parsed)
			result.Data = parsed
		}
		return result

	case "gemini_search":
		data, err := s.geminiSearch(op.Query, op.MaxTokens)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Success = true
		if op.Verbatim {
			result.Data = data
		}
		s.recordChange(txn, "gemini_search", op.Tree, "")
		return result

	default:
		result.Error = fmt.Sprintf("unknown operation: %s", op.Operation)
		return result
	}
}

// isModifyingOperation returns true if the operation modifies tree data
func isModifyingOperation(operation string) bool {
	switch operation {
	case "create_tree", "delete_tree", "set", "delete", "append", "prepend", "transform", "gemini_search":
		return true
	default:
		return false
	}
}
