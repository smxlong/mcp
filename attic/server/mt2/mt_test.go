package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create test server with temp directory
func newTestServer(t *testing.T) (*MTServer, func()) {
	tmpDir, err := os.MkdirTemp("", "mt2-test-*")
	require.NoError(t, err)

	server, err := NewMTServer(tmpDir)
	require.NoError(t, err)

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return server, cleanup
}

// Helper to create test request (batch-only mode)
func makeRequest(operation string, params map[string]any) mcp.CallToolRequest {
	params["operation"] = operation
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"operations": []map[string]any{params},
			},
		},
	}
}

// Helper to extract JSON data from tool result (batch-only mode)
func extractData(t *testing.T, result *mcp.CallToolResult) map[string]any {
	require.NotEmpty(t, result.Content)

	content, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)

	var parsed map[string]any
	err := json.Unmarshal([]byte(content.Text), &parsed)
	require.NoError(t, err)

	return parsed
}

// Helper to extract first operation result from batch response
func extractFirstResult(t *testing.T, batchData map[string]any) map[string]any {
	// In batch mode, extract the first result from the results array if present
	if results, ok := batchData["results"].([]interface{}); ok && len(results) > 0 {
		if firstResult, ok := results[0].(map[string]interface{}); ok {
			// Convert to map[string]any
			converted := make(map[string]any)
			for k, v := range firstResult {
				converted[k] = v
			}
			return converted
		}
	}
	return batchData
}

// Helper to check if batch operation succeeded
func batchSucceeded(t *testing.T, result *mcp.CallToolResult) bool {
	data := extractData(t, result)
	success, ok := data["success"].(bool)
	return ok && success
}

// Helper to check if batch operation failed
func batchFailed(t *testing.T, result *mcp.CallToolResult) bool {
	return !batchSucceeded(t, result)
}

// Helper to extract data field from result and unmarshal it
func extractDataField(t *testing.T, result map[string]any) string {
	if result["data"] == nil {
		return ""
	}
	// Data comes back as interface{}, need to marshal/unmarshal
	bytes, err := json.Marshal(result["data"])
	require.NoError(t, err)
	// Remove quotes if it's a JSON string
	str := string(bytes)
	if len(str) > 0 && str[0] == '"' {
		var unquoted string
		json.Unmarshal(bytes, &unquoted)
		return unquoted
	}
	return str
}

func TestCreateTree(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("create empty tree", func(t *testing.T) {
		req := makeRequest("create_tree", map[string]any{"tree": "test1"})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		data := extractData(t, result)
		assert.True(t, data["success"].(bool))
	})

	t.Run("create tree with initial data", func(t *testing.T) {
		req := makeRequest("create_tree", map[string]any{
			"tree": "test2",
			"data": map[string]any{"hello": "world"},
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		data := extractData(t, result)
		assert.True(t, data["success"].(bool))

		// Verify data
		req2 := makeRequest("get", map[string]any{"tree": "test2", "path": "."})
		result2, err := server.handleMT(ctx, req2)
		require.NoError(t, err)
		batchData := extractData(t, result2)
		data2 := extractFirstResult(t, batchData)
		assert.True(t, batchData["success"].(bool))
		// data field is JSON bytes, parse it
		dataBytes, _ := json.Marshal(data2["data"])
		var parsed map[string]any
		json.Unmarshal(dataBytes, &parsed)
		assert.Equal(t, "world", parsed["hello"])
	})

	t.Run("duplicate tree fails", func(t *testing.T) {
		req := makeRequest("create_tree", map[string]any{"tree": "test3"})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		assert.True(t, batchSucceeded(t, result))

		// Try to create again
		result2, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		assert.True(t, batchFailed(t, result2))
	})
}

func TestPathRequired(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Create tree
	req := makeRequest("create_tree", map[string]any{"tree": "test"})
	_, err := server.handleMT(ctx, req)
	require.NoError(t, err)

	t.Run("get requires path", func(t *testing.T) {
		req := makeRequest("get", map[string]any{"tree": "test"})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		assert.True(t, batchFailed(t, result))
	})

	t.Run("set requires path", func(t *testing.T) {
		req := makeRequest("set", map[string]any{"tree": "test", "value": 42})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		assert.True(t, batchFailed(t, result))
	})

	t.Run("delete requires path", func(t *testing.T) {
		req := makeRequest("delete", map[string]any{"tree": "test"})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		assert.True(t, batchFailed(t, result))
	})
}

func TestSetAndGet(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Create tree
	req := makeRequest("create_tree", map[string]any{"tree": "test"})
	_, err := server.handleMT(ctx, req)
	require.NoError(t, err)

	t.Run("set and get value", func(t *testing.T) {
		// Set value
		req := makeRequest("set", map[string]any{
			"tree":  "test",
			"path":  ".name",
			"value": "Alice",
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		data := extractData(t, result)
		assert.True(t, data["success"].(bool))

		// Get value
		req2 := makeRequest("get", map[string]any{
			"tree": "test",
			"path": ".name",
		})
		result2, err := server.handleMT(ctx, req2)
		require.NoError(t, err)
		batchData := extractData(t, result2)
		data2 := extractFirstResult(t, batchData)
		dataStr := extractDataField(t, data2)
		assert.Contains(t, dataStr, "Alice")
	})
}

func TestVerbatimFlag(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Create tree with data
	req := makeRequest("create_tree", map[string]any{
		"tree": "test",
		"data": map[string]any{"value": 42},
	})
	_, err := server.handleMT(ctx, req)
	require.NoError(t, err)

	t.Run("get defaults to verbatim=true", func(t *testing.T) {
		req := makeRequest("get", map[string]any{"tree": "test", "path": "."})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		batchData := extractData(t, result)
		data := extractFirstResult(t, batchData)
		assert.NotNil(t, data["data"])
	})

	t.Run("get with verbatim=false", func(t *testing.T) {
		req := makeRequest("get", map[string]any{
			"tree":     "test",
			"path":     ".",
			"verbatim": false,
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		batchData := extractData(t, result)
		// When verbatim=false, results array should be empty
		results, ok := batchData["results"].([]interface{})
		assert.True(t, !ok || len(results) == 0)
	})

	t.Run("set defaults to verbatim=false", func(t *testing.T) {
		req := makeRequest("set", map[string]any{
			"tree":  "test",
			"path":  ".value",
			"value": 100,
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		batchData := extractData(t, result)
		// When verbatim=false (default for set), results array should be empty
		results, ok := batchData["results"].([]interface{})
		assert.True(t, !ok || len(results) == 0)
	})

	t.Run("set with verbatim=true", func(t *testing.T) {
		req := makeRequest("set", map[string]any{
			"tree":     "test",
			"path":     ".value",
			"value":    200,
			"verbatim": true,
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		batchData := extractData(t, result)
		// set operation doesn't return data, even with verbatim=true
		// because it's not a read operation
		data := extractFirstResult(t, batchData)
		assert.Nil(t, data["data"])
	})
}

func TestWindow(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Create tree
	req := makeRequest("create_tree", map[string]any{"tree": "test"})
	_, err := server.handleMT(ctx, req)
	require.NoError(t, err)

	// Initialize array
	req = makeRequest("set", map[string]any{
		"tree":  "test",
		"path":  ".items",
		"value": []int{},
	})
	_, err = server.handleMT(ctx, req)
	require.NoError(t, err)

	t.Run("append with window=1", func(t *testing.T) {
		req := makeRequest("append", map[string]any{
			"tree":   "test",
			"path":   ".items",
			"value":  1,
			"window": 1,
		})
		_, err := server.handleMT(ctx, req)
		require.NoError(t, err)

		req = makeRequest("append", map[string]any{
			"tree":   "test",
			"path":   ".items",
			"value":  2,
			"window": 1,
		})
		_, err = server.handleMT(ctx, req)
		require.NoError(t, err)

		// Should only have last item
		req2 := makeRequest("get", map[string]any{"tree": "test", "path": ".items"})
		result, err := server.handleMT(ctx, req2)
		require.NoError(t, err)
		batchData := extractData(t, result)
		data := extractFirstResult(t, batchData)
		dataStr := extractDataField(t, data)
		assert.Contains(t, dataStr, "[2]")
	})

	t.Run("negative window fails", func(t *testing.T) {
		req := makeRequest("append", map[string]any{
			"tree":   "test",
			"path":   ".items",
			"value":  3,
			"window": -1,
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		assert.True(t, batchFailed(t, result))
	})
}

func TestTransformCrossTree(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Create source tree
	req := makeRequest("create_tree", map[string]any{
		"tree": "source",
		"data": map[string]any{"users": []map[string]any{
			{"name": "Alice", "age": 30},
			{"name": "Bob", "age": 25},
		}},
	})
	_, err := server.handleMT(ctx, req)
	require.NoError(t, err)

	// Create target tree
	req = makeRequest("create_tree", map[string]any{"tree": "target"})
	_, err = server.handleMT(ctx, req)
	require.NoError(t, err)

	t.Run("transform from source to target", func(t *testing.T) {
		req := makeRequest("transform", map[string]any{
			"source_tree": "source",
			"source_path": ".users",
			"tree":        "target",
			"target_path": ".names",
			"filter":      "[.[].name]",
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err)
		data := extractData(t, result)
		assert.True(t, data["success"].(bool))

		// Verify target has transformed data
		req2 := makeRequest("get", map[string]any{"tree": "target", "path": ".names"})
		result2, err := server.handleMT(ctx, req2)
		require.NoError(t, err)
		batchData := extractData(t, result2)
		data2 := extractFirstResult(t, batchData)
		dataStr := extractDataField(t, data2)
		assert.Contains(t, dataStr, "Alice")
		assert.Contains(t, dataStr, "Bob")
	})
}

func TestBatchSequential(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()

	t.Run("batch operations see previous state", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{
					"operations": []map[string]any{
						{
							"operation": "create_tree",
							"tree":      "test",
						},
						{
							"operation": "set",
							"tree":      "test",
							"path":      ".counter",
							"value":     1,
						},
						{
							"operation": "get",
							"tree":      "test",
							"path":      ".counter",
						},
						{
							"operation": "set",
							"tree":      "test",
							"path":      ".counter",
							"value":     2,
						},
						{
							"operation": "get",
							"tree":      "test",
							"path":      ".counter",
						},
					},
				},
			},
		}

		result, err := server.handleMT(context.Background(), req)
		require.NoError(t, err)
		data := extractData(t, result)
		assert.True(t, data["success"].(bool))
		assert.Equal(t, float64(5), data["total_operations"])
		assert.Equal(t, float64(5), data["successful_operations"])

		// Check that we got results from get operations
		results := data["results"].([]interface{})
		assert.Equal(t, 2, len(results)) // Two get operations
	})

	t.Run("batch stops on error", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{
					"operations": []map[string]any{
						{
							"operation": "create_tree",
							"tree":      "test2",
						},
						{
							"operation": "get",
							"tree":      "nonexistent",
							"path":      ".",
						},
						{
							"operation": "set",
							"tree":      "test2",
							"path":      ".should_not_execute",
							"value":     "skipped",
						},
					},
				},
			},
		}

		result, err := server.handleMT(context.Background(), req)
		require.NoError(t, err)
		data := extractData(t, result)
		assert.False(t, data["success"].(bool))
		assert.Equal(t, float64(1), data["successful_operations"])
		assert.Equal(t, float64(2), data["failed_operations"])
	})
}

func TestOperationHook(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Track hook calls
	var hookCalls []string
	server.SetHook(func(operation, tree, path string) {
		hookCalls = append(hookCalls, fmt.Sprintf("%s:%s:%s", operation, tree, path))
	})

	t.Run("hook called between batch operations", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{
					"operations": []map[string]any{
						{
							"operation": "create_tree",
							"tree":      "test",
						},
						{
							"operation": "set",
							"tree":      "test",
							"path":      ".value",
							"value":     42,
						},
					},
				},
			},
		}

		_, err := server.handleMT(ctx, req)
		require.NoError(t, err)

		assert.Equal(t, 2, len(hookCalls))
		assert.Equal(t, "create_tree:test:", hookCalls[0])
		assert.Equal(t, "set:test:.value", hookCalls[1])
	})
}

func TestPersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mt2-persist-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	t.Run("data persists across server restarts", func(t *testing.T) {
		// Create server and add data
		server1, err := NewMTServer(tmpDir)
		require.NoError(t, err)

		req := makeRequest("create_tree", map[string]any{
			"tree": "test",
			"data": map[string]any{"persisted": true},
		})
		_, err = server1.handleMT(ctx, req)
		require.NoError(t, err)

		// Create new server instance
		server2, err := NewMTServer(tmpDir)
		require.NoError(t, err)

		// Verify data was loaded
		req2 := makeRequest("get", map[string]any{"tree": "test", "path": "."})
		result, err := server2.handleMT(ctx, req2)
		require.NoError(t, err)
		batchData := extractData(t, result)
		data := extractFirstResult(t, batchData)
		dataStr := extractDataField(t, data)
		assert.Contains(t, dataStr, "persisted")
	})
}
