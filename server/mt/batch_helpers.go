package main

import (
	"encoding/json"
	"fmt"
)

// Helper methods for batch operations that need unlocked variants

// getUnlockedHelper is a wrapper for getUnlocked that already exists
// This ensures consistency in batch operations
func (s *MTServer) getUnlockedHelper(tree, path string) (json.RawMessage, error) {
	return s.getUnlocked(tree, path)
}

// setUnlockedHelper is a wrapper for setUnlocked that already exists
// This ensures consistency in batch operations
func (s *MTServer) setUnlockedHelper(tree, path string, value json.RawMessage) error {
	return s.setUnlocked(tree, path, value)
}

// deleteUnlockedHelper is a wrapper for deleteUnlocked that already exists
// This ensures consistency in batch operations
func (s *MTServer) deleteUnlockedHelper(tree, path string) error {
	return s.deleteUnlocked(tree, path)
}

// Batch-specific validation helpers

// validateBatchOperation validates a single batch operation
func validateBatchOperation(op BatchOperation) error {
	if op.Operation == "" {
		return fmt.Errorf("operation is required")
	}

	// Operations that require tree parameter
	requiresTree := []string{
		"create_tree", "delete_tree", "get", "set", "delete",
		"append", "prepend", "transform", "query", "gemini_search",
	}

	for _, reqOp := range requiresTree {
		if op.Operation == reqOp && op.Tree == "" {
			return fmt.Errorf("operation %s requires tree parameter", op.Operation)
		}
	}

	return nil
}

// prepareBatchValue prepares a value for batch operations
func prepareBatchValue(value json.RawMessage) json.RawMessage {
	if value == nil {
		return json.RawMessage("null")
	}
	return value
}
