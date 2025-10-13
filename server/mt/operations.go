package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// execJQ executes jq with input and filter, returns output
func (s *MTServer) execJQ(input []byte, filter string) ([]byte, error) {
	if filter == "" {
		filter = "."
	}
	cmd := exec.Command("jq", "-c", filter)
	cmd.Stdin = strings.NewReader(string(input))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("jq error: %s", strings.TrimSpace(string(output)))
	}
	return output, nil
}

// jqSet sets value at path using jq
func jqSet(input []byte, path, value string) ([]byte, error) {
	if path == "" || path == "." {
		return []byte(value), nil
	}
	// Use jq's setpath function for safe nested updates
	filter := fmt.Sprintf("setpath(%s; %s)", pathToArray(path), value)
	cmd := exec.Command("jq", "-c", filter)
	cmd.Stdin = strings.NewReader(string(input))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("jq error: %s", strings.TrimSpace(string(output)))
	}
	return output, nil
}

// pathToArray converts jq path to array format for setpath
func pathToArray(path string) string {
	if path == "." {
		return "[]"
	}
	// Simple conversion: .a.b.c -> ["a","b","c"]
	parts := strings.Split(strings.TrimPrefix(path, "."), ".")
	quoted := make([]string, len(parts))
	for i, p := range parts {
		// Handle array indices
		if strings.Contains(p, "[") {
			p = strings.ReplaceAll(p, "[", `",`)
			p = strings.ReplaceAll(p, "]", `,"`)
			quoted[i] = p
		} else {
			quoted[i] = fmt.Sprintf(`"%s"`, p)
		}
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

// createTree creates a new tree with optional initial data
func (s *MTServer) createTree(tree string, data json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data == nil {
		data = json.RawMessage("{}")
	}
	s.trees[tree] = &MemoryTree{Name: tree, Data: data}
	return nil
}

// deleteTree removes a tree
func (s *MTServer) deleteTree(tree string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.trees[tree]; !exists {
		return fmt.Errorf("tree not found: %s", tree)
	}
	delete(s.trees, tree)
	return nil
}

// listTrees returns metadata about all trees
func (s *MTServer) listTrees() []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]map[string]any, 0, len(s.trees))
	for name := range s.trees {
		result = append(result, map[string]any{
			"name": name,
			"size": len(s.trees[name].Data),
		})
	}
	return result
}

// get retrieves data at path using jq
func (s *MTServer) get(tree, path string) (json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getUnlocked(tree, path)
}

// getUnlocked performs get without acquiring lock (for internal use)
func (s *MTServer) getUnlocked(tree, path string) (json.RawMessage, error) {
	t, exists := s.trees[tree]
	if !exists {
		return nil, fmt.Errorf("tree not found: %s", tree)
	}
	if path == "" {
		path = "."
	}
	result, err := s.execJQ(t.Data, path)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(result), nil
}

// set updates data at path using jq
func (s *MTServer) set(tree, path string, value json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setUnlocked(tree, path, value)
}

// setUnlocked performs set without acquiring lock (for internal use)
func (s *MTServer) setUnlocked(tree, path string, value json.RawMessage) error {
	t, exists := s.trees[tree]
	if !exists {
		return fmt.Errorf("tree not found: %s", tree)
	}
	valueStr := string(value)
	if path == "" || path == "." {
		t.Data = value
		return nil
	}
	result, err := jqSet(t.Data, path, valueStr)
	if err != nil {
		return err
	}
	t.Data = json.RawMessage(result)
	return nil
}

// delete removes data at path using jq
func (s *MTServer) delete(tree, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteUnlocked(tree, path)
}

// deleteUnlocked performs delete without acquiring lock (for internal use)
func (s *MTServer) deleteUnlocked(tree, path string) error {
	t, exists := s.trees[tree]
	if !exists {
		return fmt.Errorf("tree not found: %s", tree)
	}
	if path == "" || path == "." {
		t.Data = json.RawMessage("{}")
		return nil
	}
	filter := fmt.Sprintf("delpaths([path(%s)])", path)
	result, err := s.execJQ(t.Data, filter)
	if err != nil {
		return err
	}
	t.Data = json.RawMessage(result)
	return nil
}

// append adds items to array at path with optional window limit
func (s *MTServer) append(tree, path string, value json.RawMessage, window int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, exists := s.trees[tree]
	if !exists {
		return fmt.Errorf("tree not found: %s", tree)
	}

	// Get current array
	current, err := s.execJQ(t.Data, path)
	if err != nil {
		// Path doesn't exist, create it as empty array
		current = []byte("[]")
	}

	// Parse value as array or single item
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err != nil {
		// Single item
		items = []json.RawMessage{value}
	}

	// Parse current array
	var arr []json.RawMessage
	if err := json.Unmarshal(current, &arr); err != nil {
		arr = []json.RawMessage{}
	}

	// Append items
	arr = append(arr, items...)

	// Apply window limit
	if window > 0 && len(arr) > window {
		arr = arr[len(arr)-window:]
	}

	// Marshal back
	newArr, _ := json.Marshal(arr)
	return s.setUnlocked(tree, path, json.RawMessage(newArr))
}

// prepend adds items to beginning of array at path with optional window limit
func (s *MTServer) prepend(tree, path string, value json.RawMessage, window int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, exists := s.trees[tree]
	if !exists {
		return fmt.Errorf("tree not found: %s", tree)
	}

	// Get current array
	current, err := s.execJQ(t.Data, path)
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

	// Apply window limit (keep first N items)
	if window > 0 && len(arr) > window {
		arr = arr[:window]
	}

	// Marshal back
	newArr, _ := json.Marshal(arr)
	return s.setUnlocked(tree, path, json.RawMessage(newArr))
}

// transform applies jq filter to source path and stores result at target path
func (s *MTServer) transform(tree, targetPath, sourcePath, filter string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, exists := s.trees[tree]
	if !exists {
		return fmt.Errorf("tree not found: %s", tree)
	}

	if sourcePath == "" {
		sourcePath = "."
	}

	// Get source data
	sourceData, err := s.execJQ(t.Data, sourcePath)
	if err != nil {
		return err
	}

	// Apply transformation
	result, err := s.execJQ(sourceData, filter)
	if err != nil {
		return err
	}

	// Set at target path
	if targetPath == "" || targetPath == "." {
		t.Data = json.RawMessage(result)
		return nil
	}

	resultStr := string(result)
	newData, err := jqSet(t.Data, targetPath, resultStr)
	if err != nil {
		return err
	}
	t.Data = json.RawMessage(newData)
	return nil
}

// query executes jq filter on tree and returns result
func (s *MTServer) query(tree, filter string) (json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, exists := s.trees[tree]
	if !exists {
		return nil, fmt.Errorf("tree not found: %s", tree)
	}
	result, err := s.execJQ(t.Data, filter)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(result), nil
}
