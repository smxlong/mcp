package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"google.golang.org/genai"
)

var safeNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// getTreePath returns the filesystem path for a tree's JSON file
func (s *MTServer) getTreePath(treeName string) (string, error) {
	if !safeNameRegex.MatchString(treeName) {
		return "", fmt.Errorf("invalid tree name: must contain only alphanumeric, underscore, or hyphen characters")
	}
	treesDir := filepath.Join(s.dataDir, "trees")
	return filepath.Join(treesDir, treeName+".json"), nil
}

// initPersistence creates the trees directory and loads existing trees
func (s *MTServer) initPersistence() error {
	treesDir := filepath.Join(s.dataDir, "trees")
	if err := os.MkdirAll(treesDir, 0755); err != nil {
		return fmt.Errorf("failed to create trees directory: %w", err)
	}

	// Load existing trees
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
			fmt.Fprintf(os.Stderr, "Warning: failed to load tree %s: %v\n", treeName, err)
			continue
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

	// Validate JSON
	var testParse any
	if err := json.Unmarshal(data, &testParse); err != nil {
		return fmt.Errorf("invalid JSON in tree file: %w", err)
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
		return fmt.Errorf("tree not found: %s", treeName)
	}

	path, err := s.getTreePath(treeName)
	if err != nil {
		return err
	}

	// Write to temporary file
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, tree.Data, 0644); err != nil {
		return fmt.Errorf("failed to write tree file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // Clean up temp file
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

// markDirty marks a tree as needing to be saved (periodic mode only)
func (s *MTServer) markDirty(treeName string) {
	if s.saveMode != "periodic" {
		return
	}

	s.dirtyTrees[treeName] = true

	// Reset the 5-second debounce timer
	if !s.saveTimer.Stop() {
		select {
		case <-s.saveTimer.C:
		default:
		}
	}
	s.saveTimer.Reset(5 * time.Second)
}

// periodicSaveLoop runs in the background and saves dirty trees periodically
func (s *MTServer) periodicSaveLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.shutdownCh:
			return
		case <-s.saveTimer.C:
			s.saveDirtyTrees()
		case <-s.saveTicker.C:
			s.saveDirtyTrees()
		}
	}
}

// saveDirtyTrees saves all trees marked as dirty
func (s *MTServer) saveDirtyTrees() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveDirtyTreesLocked()
}

// saveDirtyTreesLocked saves dirty trees without acquiring the lock
func (s *MTServer) saveDirtyTreesLocked() error {
	if len(s.dirtyTrees) == 0 {
		return nil
	}

	var lastErr error
	for treeName := range s.dirtyTrees {
		if err := s.saveTree(treeName); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving tree %s: %v\n", treeName, err)
			lastErr = err
		}
	}

	// Clear dirty flags after attempting to save
	s.dirtyTrees = make(map[string]bool)

	return lastErr
}

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

	// Persist immediately or mark dirty
	if s.saveMode == "immediate" {
		return s.saveTree(tree)
	}
	s.markDirty(tree)
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

	// Delete from disk
	if err := s.deleteTreeFile(tree); err != nil {
		return err
	}

	// Remove from dirty set if present
	delete(s.dirtyTrees, tree)

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
	if err := s.setUnlocked(tree, path, value); err != nil {
		return err
	}

	// Persist immediately or mark dirty
	if s.saveMode == "immediate" {
		return s.saveTree(tree)
	}
	s.markDirty(tree)
	return nil
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
	if err := s.deleteUnlocked(tree, path); err != nil {
		return err
	}

	// Persist immediately or mark dirty
	if s.saveMode == "immediate" {
		return s.saveTree(tree)
	}
	s.markDirty(tree)
	return nil
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
	if err := s.setUnlocked(tree, path, json.RawMessage(newArr)); err != nil {
		return err
	}

	// Persist immediately or mark dirty
	if s.saveMode == "immediate" {
		return s.saveTree(tree)
	}
	s.markDirty(tree)
	return nil
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
	if err := s.setUnlocked(tree, path, json.RawMessage(newArr)); err != nil {
		return err
	}

	// Persist immediately or mark dirty
	if s.saveMode == "immediate" {
		return s.saveTree(tree)
	}
	s.markDirty(tree)
	return nil
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
	} else {
		resultStr := string(result)
		newData, err := jqSet(t.Data, targetPath, resultStr)
		if err != nil {
			return err
		}
		t.Data = json.RawMessage(newData)
	}

	// Persist immediately or mark dirty
	if s.saveMode == "immediate" {
		return s.saveTree(tree)
	}
	s.markDirty(tree)
	return nil
}

// query executes jq filter on tree and returns result
func (s *MTServer) query(tree, filter string) (json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queryUnlocked(tree, filter)
}

func (s *MTServer) queryUnlocked(tree, filter string) (json.RawMessage, error) {
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

// geminiSearch performs a Google search using Gemini AI with GoogleSearch tool
func (s *MTServer) geminiSearch(query string, maxTokens int) (any, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: os.Getenv("GEMINI_API_KEY"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}
	response, err := client.Models.GenerateContent(ctx, "gemini-2.5-flash", []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				{
					Text: fmt.Sprintf("Use the GoogleSearch tool to search for: %s", query),
				},
			},
		},
	}, &genai.GenerateContentConfig{
		MaxOutputTokens: int32(maxTokens),
		Tools: []*genai.Tool{
			{
				GoogleSearch: &genai.GoogleSearch{},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("gemini generate error: %w", err)
	}
	return response, nil
}
