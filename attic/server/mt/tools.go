package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// createMTTool defines the unified mt tool schema
func createMTTool() mcp.Tool {
	return mcp.Tool{
		Name:        "mt",
		Description: "Unified memory tree operations supporting single commands or batch arrays of commands (max 32 operations per batch)",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"operation": map[string]any{
					"type":        "string",
					"enum":        []string{"create_tree", "delete_tree", "list_trees", "get", "set", "delete", "append", "prepend", "transform", "query", "gemini_search"},
					"description": "Single operation to perform (mutually exclusive with operations array)",
				},
				"operations": map[string]any{
					"type":        "array",
					"description": "Array of operations to perform in sequence (mutually exclusive with operation, max 32 operations)",
					"maxItems":    MaxBatchOperations,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"operation": map[string]any{
								"type":        "string",
								"enum":        []string{"create_tree", "delete_tree", "list_trees", "get", "set", "delete", "append", "prepend", "transform", "query", "gemini_search"},
								"description": "Operation to perform",
							},
							"tree": map[string]any{
								"type":        "string",
								"description": "Tree name",
							},
							"path": map[string]any{
								"type":        "string",
								"description": "jq path expression (default: '.')",
							},
							"value": map[string]any{
								"description": "Value for operation (JSON)",
							},
							"data": map[string]any{
								"description": "Initial data for create_tree (JSON)",
							},
							"filter": map[string]any{
								"type":        "string",
								"description": "jq filter expression",
							},
							"source_path": map[string]any{
								"type":        "string",
								"description": "Source path for transform operation (default: '.')",
							},
							"window": map[string]any{
								"type":        "integer",
								"description": "Maximum array length for append/prepend operations",
							},
							"query": map[string]any{
								"type":        "string",
								"description": "Search query for gemini_search operation",
							},
							"max_tokens": map[string]any{
								"type":        "integer",
								"description": "Maximum number of tokens for gemini_search operation",
							},
							"verbatim": map[string]any{
								"type":        "boolean",
								"description": "Whether to echo result back in tool response (default: false)",
								"default":     false,
							},
						},
						"required": []string{"operation"},
					},
				},
				"continue_after_errors": map[string]any{
					"type":        "boolean",
					"description": "Whether to continue executing remaining operations after an error (default: false)",
					"default":     false,
				},
				"tree": map[string]any{
					"type":        "string",
					"description": "Tree name (required for most operations)",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "jq path expression (default: '.')",
				},
				"value": map[string]any{
					"description": "Value to set/append/prepend (JSON)",
				},
				"data": map[string]any{
					"description": "Initial data for create_tree (JSON)",
				},
				"filter": map[string]any{
					"type":        "string",
					"description": "jq filter expression for query/transform operations",
				},
				"source_path": map[string]any{
					"type":        "string",
					"description": "Source path for transform operation (default: '.')",
				},
				"window": map[string]any{
					"type":        "integer",
					"description": "Maximum array length for append/prepend operations",
				},
				"query": map[string]any{
					"type":        "string",
					"description": "Search query for gemini_search operation",
				},
				"max_tokens": map[string]any{
					"type":        "integer",
					"description": "Maximum number of tokens for gemini_search operation",
				},
			},
			Required: []string{"operation"},
		},
	}
}

// handleMT dispatches mt tool operations to appropriate methods
func (s *MTServer) handleMT(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check if this is a batch operation
	if operationsArray := request.GetArguments()["operations"]; operationsArray != nil {
		return s.handleBatchMT(ctx, request)
	}

	operation := request.GetString("operation", "")
	if operation == "" {
		return mcp.NewToolResultError("operation or operations array required"), nil
	}

	tree := request.GetString("tree", "")

	var result any
	var err error

	switch operation {
	case "create_tree":
		if tree == "" {
			return mcp.NewToolResultError("tree parameter required"), nil
		}
		var data json.RawMessage
		if dataVal := request.GetArguments()["data"]; dataVal != nil {
			if dataBytes, e := json.Marshal(dataVal); e == nil {
				data = dataBytes
			}
		}
		err = s.createTree(tree, data)
		result = map[string]any{"success": true, "tree": tree}

	case "delete_tree":
		if tree == "" {
			return mcp.NewToolResultError("tree parameter required"), nil
		}
		err = s.deleteTree(tree)
		result = map[string]any{"success": true, "tree": tree}

	case "list_trees":
		result = map[string]any{"success": true, "trees": s.listTrees()}

	case "get":
		if tree == "" {
			return mcp.NewToolResultError("tree parameter required"), nil
		}
		path := request.GetString("path", ".")
		var data json.RawMessage
		data, err = s.get(tree, path)
		if err == nil {
			var parsed any
			json.Unmarshal(data, &parsed)
			result = map[string]any{"success": true, "tree": tree, "path": path, "data": parsed}
		}

	case "set":
		if tree == "" {
			return mcp.NewToolResultError("tree parameter required"), nil
		}
		path := request.GetString("path", ".")
		valueArg := request.GetArguments()["value"]
		if valueArg == nil {
			return mcp.NewToolResultError("value parameter required"), nil
		}
		valueBytes, _ := json.Marshal(valueArg)
		err = s.set(tree, path, json.RawMessage(valueBytes))
		result = map[string]any{"success": true, "tree": tree, "path": path}

	case "delete":
		if tree == "" {
			return mcp.NewToolResultError("tree parameter required"), nil
		}
		path := request.GetString("path", ".")
		err = s.delete(tree, path)
		result = map[string]any{"success": true, "tree": tree, "path": path}

	case "append":
		if tree == "" {
			return mcp.NewToolResultError("tree parameter required"), nil
		}
		path := request.GetString("path", ".")
		valueArg := request.GetArguments()["value"]
		if valueArg == nil {
			return mcp.NewToolResultError("value parameter required"), nil
		}
		valueBytes, _ := json.Marshal(valueArg)
		window := request.GetInt("window", 0)
		err = s.append(tree, path, json.RawMessage(valueBytes), window)
		result = map[string]any{"success": true, "tree": tree, "path": path, "window": window}

	case "prepend":
		if tree == "" {
			return mcp.NewToolResultError("tree parameter required"), nil
		}
		path := request.GetString("path", ".")
		valueArg := request.GetArguments()["value"]
		if valueArg == nil {
			return mcp.NewToolResultError("value parameter required"), nil
		}
		valueBytes, _ := json.Marshal(valueArg)
		window := request.GetInt("window", 0)
		err = s.prepend(tree, path, json.RawMessage(valueBytes), window)
		result = map[string]any{"success": true, "tree": tree, "path": path, "window": window}

	case "transform":
		if tree == "" {
			return mcp.NewToolResultError("tree parameter required"), nil
		}
		targetPath := request.GetString("path", ".")
		sourcePath := request.GetString("source_path", ".")
		filter := request.GetString("filter", "")
		if filter == "" {
			return mcp.NewToolResultError("filter parameter required"), nil
		}
		err = s.transform(tree, targetPath, sourcePath, filter)
		result = map[string]any{"success": true, "tree": tree, "path": targetPath}

	case "query":
		if tree == "" {
			return mcp.NewToolResultError("tree parameter required"), nil
		}
		filter := request.GetString("filter", ".")
		var data json.RawMessage
		data, err = s.query(tree, filter)
		if err == nil {
			var parsed any
			json.Unmarshal(data, &parsed)
			result = map[string]any{"success": true, "tree": tree, "filter": filter, "result": parsed}
		}

	case "gemini_search":
		query := request.GetString("query", "")
		if query == "" {
			return mcp.NewToolResultError("query parameter required"), nil
		}
		maxTokens := request.GetInt("max_tokens", 1000) // Default to 1000 tokens if not specified
		var searchResult any
		searchResult, err = s.geminiSearch(query, maxTokens)
		if err == nil {
			result = map[string]any{"success": true, "query": query, "max_tokens": maxTokens, "result": searchResult}
		}

	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown operation: %s", operation)), nil
	}

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("operation failed: %v", err)), nil
	}

	return mcp.NewToolResultStructuredOnly(result), nil
}
