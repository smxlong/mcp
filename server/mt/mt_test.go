package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create test server with temp directory
func newTestServer(t *testing.T) (*MTServer, func()) {
	tmpDir, err := os.MkdirTemp("", "mt-test-*")
	require.NoError(t, err, "Failed to create temp directory")
	
	server, err := NewMTServer(tmpDir, "immediate")
	require.NoError(t, err, "Failed to create server")
	
	cleanup := func() {
		server.Shutdown()
		os.RemoveAll(tmpDir)
	}
	
	return server, cleanup
}

// Helper function to create test request
func makeRequest(operation string, params map[string]any) mcp.CallToolRequest {
	params["operation"] = operation
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: params,
		},
	}
}

// Helper to extract JSON data from tool result
func extractData(t *testing.T, result *mcp.CallToolResult) map[string]any {
	require.False(t, result.IsError, "Tool result should not be an error")
	require.NotEmpty(t, result.Content, "Tool result should have content")
	
	content, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "Content should be TextContent type")
	
	var parsed map[string]any
	err := json.Unmarshal([]byte(content.Text), &parsed)
	require.NoError(t, err, "Should parse JSON result")
	
	return parsed
}

func TestCreateTree(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("create empty tree", func(t *testing.T) {
		req := makeRequest("create_tree", map[string]any{
			"tree": "test",
		})
		result, err := server.handleMT(ctx, req)
		
		require.NoError(t, err, "Should not return error")
		require.False(t, result.IsError, "Should not return error result")
		
		// Verify tree exists in server state
		server.mu.RLock()
		_, exists := server.trees["test"]
		server.mu.RUnlock()
		assert.True(t, exists, "Tree should be created in server state")
	})

	t.Run("create tree with initial data", func(t *testing.T) {
		initialData := map[string]any{"key": "value", "count": 42}
		req := makeRequest("create_tree", map[string]any{
			"tree": "test_with_data",
			"data": initialData,
		})
		result, err := server.handleMT(ctx, req)
		
		require.NoError(t, err, "Should not return error")
		require.False(t, result.IsError, "Should not return error result")
		
		// Verify data was stored correctly
		server.mu.RLock()
		tree, exists := server.trees["test_with_data"]
		server.mu.RUnlock()
		
		require.True(t, exists, "Tree should exist")
		
		var stored map[string]any
		err = json.Unmarshal(tree.Data, &stored)
		require.NoError(t, err, "Should parse stored data")
		assert.Equal(t, "value", stored["key"], "Should store string value")
		assert.Equal(t, float64(42), stored["count"], "Should store numeric value")
	})
}

func TestDeleteTree(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("delete existing tree", func(t *testing.T) {
		// Create a tree first
		req := makeRequest("create_tree", map[string]any{"tree": "to_delete"})
		server.handleMT(ctx, req)

		// Delete it
		req = makeRequest("delete_tree", map[string]any{"tree": "to_delete"})
		result, err := server.handleMT(ctx, req)
		
		require.NoError(t, err, "Should not return error")
		require.False(t, result.IsError, "Should not return error result")
		
		// Verify it's gone
		server.mu.RLock()
		_, exists := server.trees["to_delete"]
		server.mu.RUnlock()
		assert.False(t, exists, "Tree should be deleted")
	})

	t.Run("delete non-existent tree", func(t *testing.T) {
		req := makeRequest("delete_tree", map[string]any{"tree": "nonexistent"})
		result, err := server.handleMT(ctx, req)
		
		require.NoError(t, err, "Should not return Go error")
		assert.True(t, result.IsError, "Should return error result for non-existent tree")
	})
}

func TestListTrees(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("list multiple trees", func(t *testing.T) {
		// Create some trees
		makeRequest("create_tree", map[string]any{"tree": "tree1", "data": map[string]any{"a": 1}})
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{"tree": "tree1"}))
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{"tree": "tree2"}))

		req := makeRequest("list_trees", map[string]any{})
		result, err := server.handleMT(ctx, req)
		
		require.NoError(t, err, "Should not return error")
		parsed := extractData(t, result)
		
		trees, ok := parsed["trees"].([]any)
		require.True(t, ok, "Should have trees array")
		assert.Equal(t, 2, len(trees), "Should list both trees")
	})
}

func TestGetAndSet(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("set and get simple value", func(t *testing.T) {
		// Create tree
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{"tree": "test"}))

		// Set a value
		req := makeRequest("set", map[string]any{
			"tree":  "test",
			"path":  ".name",
			"value": "John",
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Set should not error")
		require.False(t, result.IsError, "Set should succeed")

		// Get the value back
		req = makeRequest("get", map[string]any{
			"tree": "test",
			"path": ".name",
		})
		result, err = server.handleMT(ctx, req)
		require.NoError(t, err, "Get should not error")
		
		parsed := extractData(t, result)
		assert.Equal(t, "John", parsed["data"], "Should retrieve the set value")
	})

	t.Run("set and get nested value", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{"tree": "nested"}))

		// Set nested value
		req := makeRequest("set", map[string]any{
			"tree":  "nested",
			"path":  ".user.age",
			"value": 30,
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Set nested should not error")
		require.False(t, result.IsError, "Set nested should succeed")

		// Get the whole tree
		req = makeRequest("get", map[string]any{
			"tree": "nested",
			"path": ".",
		})
		result, err = server.handleMT(ctx, req)
		require.NoError(t, err, "Get root should not error")
		
		parsed := extractData(t, result)
		data, ok := parsed["data"].(map[string]any)
		require.True(t, ok, "Data should be an object")
		
		user, ok := data["user"].(map[string]any)
		require.True(t, ok, "User should be an object")
		assert.Equal(t, float64(30), user["age"], "Should have nested age value")
	})

	t.Run("set multiple values", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{"tree": "multi"}))

		// Set multiple values
		server.handleMT(ctx, makeRequest("set", map[string]any{
			"tree": "multi", "path": ".name", "value": "Alice",
		}))
		server.handleMT(ctx, makeRequest("set", map[string]any{
			"tree": "multi", "path": ".age", "value": 25,
		}))
		server.handleMT(ctx, makeRequest("set", map[string]any{
			"tree": "multi", "path": ".active", "value": true,
		}))

		// Get all
		req := makeRequest("get", map[string]any{"tree": "multi", "path": "."})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Get should not error")
		
		parsed := extractData(t, result)
		data, ok := parsed["data"].(map[string]any)
		require.True(t, ok, "Data should be an object")
		
		assert.Equal(t, "Alice", data["name"], "Should have name")
		assert.Equal(t, float64(25), data["age"], "Should have age")
		assert.Equal(t, true, data["active"], "Should have active flag")
	})
}

func TestDelete(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("delete key from object", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "test",
			"data": map[string]any{"a": 1, "b": 2, "c": 3},
		}))

		// Delete a key
		req := makeRequest("delete", map[string]any{
			"tree": "test",
			"path": ".b",
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Delete should not error")
		require.False(t, result.IsError, "Delete should succeed")

		// Verify it's gone
		req = makeRequest("get", map[string]any{"tree": "test", "path": "."})
		result, err = server.handleMT(ctx, req)
		require.NoError(t, err, "Get should not error")
		
		parsed := extractData(t, result)
		data, ok := parsed["data"].(map[string]any)
		require.True(t, ok, "Data should be an object")
		
		_, exists := data["b"]
		assert.False(t, exists, "Key 'b' should be deleted")
		assert.Equal(t, float64(1), data["a"], "Key 'a' should remain")
		assert.Equal(t, float64(3), data["c"], "Key 'c' should remain")
	})
}

func TestAppend(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("append single item to array", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "test",
			"data": map[string]any{"items": []int{1, 2}},
		}))

		// Append single item
		req := makeRequest("append", map[string]any{
			"tree":  "test",
			"path":  ".items",
			"value": 3,
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Append should not error")
		require.False(t, result.IsError, "Append should succeed")

		// Verify via get
		req = makeRequest("get", map[string]any{"tree": "test", "path": ".items"})
		result, err = server.handleMT(ctx, req)
		require.NoError(t, err, "Get should not error")
		
		parsed := extractData(t, result)
		items, ok := parsed["data"].([]any)
		require.True(t, ok, "Data should be an array")
		assert.Equal(t, 3, len(items), "Should have 3 items")
		assert.Equal(t, float64(3), items[2], "Last item should be 3")
	})

	t.Run("append multiple items", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "test2",
			"data": map[string]any{"items": []int{1}},
		}))

		req := makeRequest("append", map[string]any{
			"tree":  "test2",
			"path":  ".items",
			"value": []int{2, 3, 4},
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Append should not error")
		require.False(t, result.IsError, "Append should succeed")

		// Verify
		req = makeRequest("get", map[string]any{"tree": "test2", "path": ".items"})
		result, err = server.handleMT(ctx, req)
		require.NoError(t, err, "Get should not error")
		
		parsed := extractData(t, result)
		items, ok := parsed["data"].([]any)
		require.True(t, ok, "Data should be an array")
		assert.Equal(t, 4, len(items), "Should have 4 items")
	})

	t.Run("append with window limit", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "test3",
			"data": map[string]any{"items": []int{1, 2, 3}},
		}))

		req := makeRequest("append", map[string]any{
			"tree":   "test3",
			"path":   ".items",
			"value":  []int{4, 5, 6},
			"window": 3,
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Append with window should not error")
		require.False(t, result.IsError, "Append with window should succeed")

		// Verify window kept last 3 items
		req = makeRequest("get", map[string]any{"tree": "test3", "path": ".items"})
		result, err = server.handleMT(ctx, req)
		require.NoError(t, err, "Get should not error")
		
		parsed := extractData(t, result)
		items, ok := parsed["data"].([]any)
		require.True(t, ok, "Data should be an array")
		require.Equal(t, 3, len(items), "Should have exactly 3 items due to window")
		assert.Equal(t, float64(4), items[0], "Should keep item 4")
		assert.Equal(t, float64(5), items[1], "Should keep item 5")
		assert.Equal(t, float64(6), items[2], "Should keep item 6")
	})
}

func TestPrepend(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("prepend single item to array", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "test",
			"data": map[string]any{"items": []int{2, 3}},
		}))

		req := makeRequest("prepend", map[string]any{
			"tree":  "test",
			"path":  ".items",
			"value": 1,
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Prepend should not error")
		require.False(t, result.IsError, "Prepend should succeed")

		// Verify
		req = makeRequest("get", map[string]any{"tree": "test", "path": ".items"})
		result, err = server.handleMT(ctx, req)
		require.NoError(t, err, "Get should not error")
		
		parsed := extractData(t, result)
		items, ok := parsed["data"].([]any)
		require.True(t, ok, "Data should be an array")
		require.Equal(t, 3, len(items), "Should have 3 items")
		assert.Equal(t, float64(1), items[0], "First item should be 1")
	})

	t.Run("prepend with window limit", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "test2",
			"data": map[string]any{"items": []int{4, 5, 6}},
		}))

		req := makeRequest("prepend", map[string]any{
			"tree":   "test2",
			"path":   ".items",
			"value":  []int{1, 2, 3},
			"window": 3,
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Prepend with window should not error")
		require.False(t, result.IsError, "Prepend with window should succeed")

		// Verify window kept first 3 items
		req = makeRequest("get", map[string]any{"tree": "test2", "path": ".items"})
		result, err = server.handleMT(ctx, req)
		require.NoError(t, err, "Get should not error")
		
		parsed := extractData(t, result)
		items, ok := parsed["data"].([]any)
		require.True(t, ok, "Data should be an array")
		require.Equal(t, 3, len(items), "Should have exactly 3 items due to window")
		assert.Equal(t, float64(1), items[0], "Should keep item 1")
		assert.Equal(t, float64(2), items[1], "Should keep item 2")
		assert.Equal(t, float64(3), items[2], "Should keep item 3")
	})
}

func TestTransform(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("transform with map operation", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "test",
			"data": map[string]any{
				"users": []map[string]any{
					{"name": "Alice", "age": 30},
					{"name": "Bob", "age": 25},
				},
			},
		}))

		// Transform: extract names
		req := makeRequest("transform", map[string]any{
			"tree":        "test",
			"path":        ".names",
			"source_path": ".users",
			"filter":      "map(.name)",
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Transform should not error")
		require.False(t, result.IsError, "Transform should succeed")

		// Verify
		req = makeRequest("get", map[string]any{"tree": "test", "path": ".names"})
		result, err = server.handleMT(ctx, req)
		require.NoError(t, err, "Get should not error")
		
		parsed := extractData(t, result)
		names, ok := parsed["data"].([]any)
		require.True(t, ok, "Data should be an array")
		require.Equal(t, 2, len(names), "Should have 2 names")
		assert.Equal(t, "Alice", names[0], "First name should be Alice")
		assert.Equal(t, "Bob", names[1], "Second name should be Bob")
	})

	t.Run("transform with filter operation", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "test2",
			"data": map[string]any{
				"users": []map[string]any{
					{"name": "Alice", "age": 30},
					{"name": "Bob", "age": 25},
					{"name": "Charlie", "age": 35},
				},
			},
		}))

		// Transform: filter users over 25
		req := makeRequest("transform", map[string]any{
			"tree":        "test2",
			"path":        ".adults",
			"source_path": ".users",
			"filter":      "map(select(.age > 25))",
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Transform should not error")
		require.False(t, result.IsError, "Transform should succeed")

		// Verify
		req = makeRequest("get", map[string]any{"tree": "test2", "path": ".adults"})
		result, err = server.handleMT(ctx, req)
		require.NoError(t, err, "Get should not error")
		
		parsed := extractData(t, result)
		adults, ok := parsed["data"].([]any)
		require.True(t, ok, "Data should be an array")
		assert.Equal(t, 2, len(adults), "Should have 2 adults (age > 25)")
	})
}

func TestQuery(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("query with aggregation", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "test",
			"data": map[string]any{
				"items": []map[string]any{
					{"id": 1, "active": true},
					{"id": 2, "active": false},
					{"id": 3, "active": true},
				},
			},
		}))

		// Query: count active items
		req := makeRequest("query", map[string]any{
			"tree":   "test",
			"filter": ".items | map(select(.active)) | length",
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Query should not error")
		
		parsed := extractData(t, result)
		count, ok := parsed["result"].(float64)
		require.True(t, ok, "Result should be a number")
		assert.Equal(t, float64(2), count, "Should have 2 active items")
	})

	t.Run("query with selection", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "test2",
			"data": map[string]any{
				"items": []map[string]any{
					{"id": 1, "status": "pending"},
					{"id": 2, "status": "complete"},
					{"id": 3, "status": "pending"},
				},
			},
		}))

		// Query: get specific item
		req := makeRequest("query", map[string]any{
			"tree":   "test2",
			"filter": ".items[] | select(.id == 2)",
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Query should not error")
		
		parsed := extractData(t, result)
		item, ok := parsed["result"].(map[string]any)
		require.True(t, ok, "Result should be an object")
		assert.Equal(t, float64(2), item["id"], "Should select item with id=2")
		assert.Equal(t, "complete", item["status"], "Should have correct status")
	})
}

func TestErrorHandling(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("missing operation parameter", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{},
			},
		}
		result, err := server.handleMT(ctx, req)
		
		require.NoError(t, err, "Should not return Go error")
		assert.True(t, result.IsError, "Should return error result for missing operation")
	})

	t.Run("unknown operation", func(t *testing.T) {
		req := makeRequest("invalid_operation", map[string]any{})
		result, err := server.handleMT(ctx, req)
		
		require.NoError(t, err, "Should not return Go error")
		assert.True(t, result.IsError, "Should return error result for unknown operation")
	})

	t.Run("missing tree parameter", func(t *testing.T) {
		req := makeRequest("get", map[string]any{})
		result, err := server.handleMT(ctx, req)
		
		require.NoError(t, err, "Should not return Go error")
		assert.True(t, result.IsError, "Should return error result for missing tree")
	})

	t.Run("non-existent tree", func(t *testing.T) {
		req := makeRequest("get", map[string]any{
			"tree": "does_not_exist",
			"path": ".",
		})
		result, err := server.handleMT(ctx, req)
		
		require.NoError(t, err, "Should not return Go error")
		assert.True(t, result.IsError, "Should return error result for non-existent tree")
	})

	t.Run("missing required value parameter", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{"tree": "test"}))
		
		req := makeRequest("set", map[string]any{
			"tree": "test",
			"path": ".field",
		})
		result, err := server.handleMT(ctx, req)
		
		require.NoError(t, err, "Should not return Go error")
		assert.True(t, result.IsError, "Should return error result for missing value")
	})
}

func TestConcurrency(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("concurrent append operations", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "concurrent",
			"data": map[string]any{"items": []int{}},
		}))

		// Run concurrent operations
		done := make(chan bool)
		for i := 0; i < 10; i++ {
			go func(n int) {
				defer func() { done <- true }()
				
				req := makeRequest("append", map[string]any{
					"tree":  "concurrent",
					"path":  ".items",
					"value": n,
				})
				result, err := server.handleMT(ctx, req)
				
				// Each operation should succeed
				assert.NoError(t, err, "Concurrent append should not error")
				assert.False(t, result.IsError, "Concurrent append should succeed")
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < 10; i++ {
			<-done
		}

		// Verify all items were added
		req := makeRequest("get", map[string]any{
			"tree": "concurrent",
			"path": ".items",
		})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Final get should not error")
		
		parsed := extractData(t, result)
		items, ok := parsed["data"].([]any)
		require.True(t, ok, "Data should be an array")
		assert.Equal(t, 10, len(items), "All 10 concurrent appends should succeed")
	})

	t.Run("concurrent mixed operations", func(t *testing.T) {
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "mixed",
			"data": map[string]any{"counter": 0, "items": []int{}},
		}))

		done := make(chan bool)
		
		// Concurrent sets
		for i := 0; i < 5; i++ {
			go func(n int) {
				defer func() { done <- true }()
				server.handleMT(ctx, makeRequest("set", map[string]any{
					"tree":  "mixed",
					"path":  ".counter",
					"value": n,
				}))
			}(i)
		}
		
		// Concurrent appends
		for i := 0; i < 5; i++ {
			go func(n int) {
				defer func() { done <- true }()
				server.handleMT(ctx, makeRequest("append", map[string]any{
					"tree":  "mixed",
					"path":  ".items",
					"value": n,
				}))
			}(i)
		}

		// Wait for all operations
		for i := 0; i < 10; i++ {
			<-done
		}

		// Tree should be in valid state (no crashes)
		req := makeRequest("get", map[string]any{"tree": "mixed", "path": "."})
		result, err := server.handleMT(ctx, req)
		require.NoError(t, err, "Should handle concurrent mixed operations")
		require.False(t, result.IsError, "Should not corrupt tree state")
	})
}

func TestPersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mt-persist-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	ctx := context.Background()

	t.Run("immediate mode - data persisted immediately", func(t *testing.T) {
		// Create server in immediate mode
		server, err := NewMTServer(tmpDir, "immediate")
		require.NoError(t, err)
		
		// Create and modify tree
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "test1",
			"data": map[string]any{"counter": 0},
		}))
		server.handleMT(ctx, makeRequest("set", map[string]any{
			"tree":  "test1",
			"path":  ".counter",
			"value": 42,
		}))
		
		// Shutdown and create new server instance
		server.Shutdown()
		
		server2, err := NewMTServer(tmpDir, "immediate")
		require.NoError(t, err)
		defer server2.Shutdown()
		
		// Verify data was loaded
		result, err := server2.handleMT(ctx, makeRequest("get", map[string]any{
			"tree": "test1",
			"path": ".counter",
		}))
		require.NoError(t, err)
		parsed := extractData(t, result)
		assert.Equal(t, float64(42), parsed["data"])
	})

	t.Run("periodic mode - data persisted on timer", func(t *testing.T) {
		tmpDir2, err := os.MkdirTemp("", "mt-periodic-*")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir2)
		
		server, err := NewMTServer(tmpDir2, "periodic")
		require.NoError(t, err)
		
		// Create tree
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "test2",
			"data": map[string]any{"value": "initial"},
		}))
		
		// Modify data
		server.handleMT(ctx, makeRequest("set", map[string]any{
			"tree":  "test2",
			"path":  ".value",
			"value": "updated",
		}))
		
		// Shutdown (triggers final flush)
		server.Shutdown()
		
		// Create new server and verify
		server2, err := NewMTServer(tmpDir2, "periodic")
		require.NoError(t, err)
		defer server2.Shutdown()
		
		result, err := server2.handleMT(ctx, makeRequest("get", map[string]any{
			"tree": "test2",
			"path": ".value",
		}))
		require.NoError(t, err)
		parsed := extractData(t, result)
		assert.Equal(t, "updated", parsed["data"])
	})

	t.Run("delete removes file", func(t *testing.T) {
		tmpDir3, err := os.MkdirTemp("", "mt-delete-*")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir3)
		
		server, err := NewMTServer(tmpDir3, "immediate")
		require.NoError(t, err)
		
		// Create and then delete tree
		server.handleMT(ctx, makeRequest("create_tree", map[string]any{"tree": "temp"}))
		server.handleMT(ctx, makeRequest("delete_tree", map[string]any{"tree": "temp"}))
		
		server.Shutdown()
		
		// Create new server - tree should not exist
		server2, err := NewMTServer(tmpDir3, "immediate")
		require.NoError(t, err)
		defer server2.Shutdown()
		
		result, err := server2.handleMT(ctx, makeRequest("list_trees", map[string]any{}))
		require.NoError(t, err)
		parsed := extractData(t, result)
		trees := parsed["trees"].([]any)
		assert.Equal(t, 0, len(trees))
	})

	t.Run("invalid tree names rejected", func(t *testing.T) {
		server, err := NewMTServer(tmpDir, "immediate")
		require.NoError(t, err)
		defer server.Shutdown()
		
		// Try to create tree with invalid characters
		result, err := server.handleMT(ctx, makeRequest("create_tree", map[string]any{
			"tree": "../../../etc/passwd",
		}))
		require.NoError(t, err)
		assert.True(t, result.IsError, "Should reject invalid tree name")
	})
}
