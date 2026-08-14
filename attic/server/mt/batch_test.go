package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// Helper to extract JSON data from tool result
func extractBatchData(t *testing.T, result *mcp.CallToolResult) map[string]any {
	require.False(t, result.IsError, "Tool result should not be an error")
	require.NotEmpty(t, result.Content, "Tool result should have content")

	content, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "Content should be TextContent type")

	var parsed map[string]any
	err := json.Unmarshal([]byte(content.Text), &parsed)
	require.NoError(t, err, "Should parse JSON result")

	return parsed
}

func TestBatchOperations(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()

	// Create server
	server, err := NewMTServer(tempDir, "immediate")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown()

	// Test basic batch operations
	t.Run("BasicBatch", func(t *testing.T) {
		// Create a batch request
		request := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{
					"operations": []map[string]any{
						{
							"operation": "create_tree",
							"tree":      "test_batch",
							"data":      map[string]any{"initial": "value"},
						},
						{
							"operation": "set",
							"tree":      "test_batch",
							"path":      ".counter",
							"value":     42,
						},
						{
							"operation": "get",
							"tree":      "test_batch",
							"path":      ".counter",
							"verbatim":  true,
						},
					},
				},
			},
		}

		result, err := server.handleMT(context.Background(), request)
		if err != nil {
			t.Fatalf("Batch operation failed: %v", err)
		}

		// Check result structure
		response := extractBatchData(t, result)

		if !response["success"].(bool) {
			t.Errorf("Expected success=true, got %v", response["success"])
		}

		if response["total_operations"].(float64) != 3 {
			t.Errorf("Expected 3 operations, got %v", response["total_operations"])
		}

		// Check that verbatim result is included
		if batchResults, exists := response["batch_results"]; exists {
			results := batchResults.([]interface{})
			if len(results) != 1 {
				t.Errorf("Expected 1 verbatim result, got %d", len(results))
			}

			firstResult := results[0].(map[string]interface{})
			if firstResult["data"].(float64) != 42 {
				t.Errorf("Expected counter value 42, got %v", firstResult["data"])
			}
		} else {
			t.Error("Expected batch_results in response")
		}
	})

	// Test error handling with continue_after_errors=false
	t.Run("StopOnError", func(t *testing.T) {
		request := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{
					"continue_after_errors": false,
					"operations": []map[string]any{
						{
							"operation": "create_tree",
							"tree":      "test_error",
						},
						{
							"operation": "get",
							"tree":      "nonexistent_tree",
							"path":      ".",
						},
						{
							"operation": "set",
							"tree":      "test_error",
							"path":      ".should_not_execute",
							"value":     "skipped",
						},
					},
				},
			},
		}

		result, err := server.handleMT(context.Background(), request)
		if err != nil {
			t.Fatalf("Batch operation failed: %v", err)
		}

		response := extractBatchData(t, result)

		if response["success"].(bool) {
			t.Error("Expected success=false due to error")
		}

		if response["successful_operations"].(float64) != 1 {
			t.Errorf("Expected 1 successful operation, got %v", response["successful_operations"])
		}
	})

	// Test error handling with continue_after_errors=true
	t.Run("ContinueAfterError", func(t *testing.T) {
		request := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{
					"continue_after_errors": true,
					"operations": []map[string]any{
						{
							"operation": "create_tree",
							"tree":      "test_continue",
						},
						{
							"operation": "get",
							"tree":      "nonexistent_tree",
							"path":      ".",
							"verbatim":  true,
						},
						{
							"operation": "set",
							"tree":      "test_continue",
							"path":      ".executed",
							"value":     "yes",
						},
					},
				},
			},
		}

		result, err := server.handleMT(context.Background(), request)
		if err != nil {
			t.Fatalf("Batch operation failed: %v", err)
		}

		response := extractBatchData(t, result)

		if response["successful_operations"].(float64) != 2 {
			t.Errorf("Expected 2 successful operations, got %v", response["successful_operations"])
		}

		if response["failed_operations"].(float64) != 1 {
			t.Errorf("Expected 1 failed operation, got %v", response["failed_operations"])
		}
	})

	// Test maximum batch size
	t.Run("MaxBatchSize", func(t *testing.T) {
		// Create 33 operations (exceeds limit of 32)
		operations := make([]map[string]any, 33)
		for i := 0; i < 33; i++ {
			operations[i] = map[string]any{
				"operation": "list_trees",
			}
		}

		request := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{
					"operations": operations,
				},
			},
		}

		result, err := server.handleMT(context.Background(), request)
		if err != nil {
			t.Fatalf("Batch operation failed: %v", err)
		}

		// Should get an error about exceeding max batch size
		if len(result.Content) == 0 || result.IsError {
			// This is expected - should reject batches > 32 operations
			return
		}

		t.Error("Expected error for batch size > 32 operations")
	})
}

func TestTransactionInterface(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()

	// Create server
	server, err := NewMTServer(tempDir, "immediate")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown()

	// Test transaction methods (should be no-ops currently)
	txn := server.beginTransaction()
	if txn == nil {
		t.Error("beginTransaction should return a transaction object")
	}

	if txn.ID == "" {
		t.Error("Transaction should have an ID")
	}

	server.recordChange(txn, "test_op", "test_tree", "test_path")
	if len(txn.Changes) != 1 {
		t.Errorf("Expected 1 recorded change, got %d", len(txn.Changes))
	}

	// Test commit (should be no-op)
	err = server.commitTransaction(txn)
	if err != nil {
		t.Errorf("commitTransaction failed: %v", err)
	}

	// Test rollback (should be no-op)
	err = server.rollbackTransaction(txn)
	if err != nil {
		t.Errorf("rollbackTransaction failed: %v", err)
	}

	// Test transaction summary
	summary := server.getTransactionSummary(txn)
	if summary["changes"].(int) != 1 {
		t.Errorf("Expected 1 change in summary, got %v", summary["changes"])
	}
}
