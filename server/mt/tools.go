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
		Description: "Unified memory tree operations supporting create, read, update, delete, array operations, transformations and queries",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"operation": map[string]any{
					"type": "string",
					"enum": []string{"create_tree", "delete_tree", "list_trees", "get", "set", "delete", "append", "prepend", "transform", "query"},
					"description": "Operation to perform",
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
			},
			Required: []string{"operation"},
		},
	}
}

// handleMT dispatches mt tool operations to appropriate methods
func (s *MTServer) handleMT(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	operation := request.GetString("operation", "")
	if operation == "" {
		return mcp.NewToolResultError("operation parameter required"), nil
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

	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown operation: %s", operation)), nil
	}

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("operation failed: %v", err)), nil
	}

	return mcp.NewToolResultStructuredOnly(result), nil
}
