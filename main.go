package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// JSON-RPC types

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP types

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    map[string]any    `json:"capabilities"`
	ServerInfo      serverInfo        `json:"serverInfo"`
}

type tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []tool `json:"tools"`
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callToolResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// Tool argument types

type readGoArgs struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

type updateGoArgs struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 10*1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		resp := handleRequest(&req)
		if resp != nil {
			out, _ := json.Marshal(resp)
			fmt.Fprintf(os.Stdout, "%s\n", out)
		}
	}
}

func handleRequest(req *jsonRPCRequest) *jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: initializeResult{
				ProtocolVersion: "2024-11-05",
				Capabilities: map[string]any{
					"tools": map[string]any{},
				},
				ServerInfo: serverInfo{
					Name:    "go-edit-mcp",
					Version: "1.0.0",
				},
			},
		}

	case "notifications/initialized":
		return nil // no response for notifications

	case "tools/list":
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: toolsListResult{
				Tools: []tool{
					{
						Name:        "ReadGo",
						Description: "Read a Go file with tabs expanded to spaces (4 spaces per tab). Returns cat -n style output with line numbers. Use this instead of the built-in Read tool for Go files to ensure whitespace consistency with UpdateGo.",
						InputSchema: map[string]any{
							"type": "object",
							"properties": map[string]any{
								"file_path": map[string]any{
									"type":        "string",
									"description": "Absolute path to the Go file",
								},
								"offset": map[string]any{
									"type":        "integer",
									"description": "1-based line number to start reading from",
								},
								"limit": map[string]any{
									"type":        "integer",
									"description": "Number of lines to read",
								},
							},
							"required": []string{"file_path"},
						},
					},
					{
						Name:        "UpdateGo",
						Description: "Search-and-replace in a Go file using whitespace-normalized matching. Strips leading/trailing whitespace from each line before matching, then re-indents the replacement to match the original file's tab-based indentation. Runs gofmt after writing; reverts on gofmt failure.",
						InputSchema: map[string]any{
							"type": "object",
							"properties": map[string]any{
								"file_path": map[string]any{
									"type":        "string",
									"description": "Absolute path to the Go file",
								},
								"old_string": map[string]any{
									"type":        "string",
									"description": "Text to find (whitespace-normalized for matching)",
								},
								"new_string": map[string]any{
									"type":        "string",
									"description": "Replacement text (will be re-indented to match original)",
								},
								"replace_all": map[string]any{
									"type":        "boolean",
									"description": "Replace all occurrences (default false)",
								},
							},
							"required": []string{"file_path", "old_string", "new_string"},
						},
					},
				},
			},
		}

	case "tools/call":
		var params callToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &jsonRPCError{Code: -32602, Message: "invalid params"},
			}
		}
		result := handleToolCall(params.Name, params.Arguments)
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}

	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32601, Message: "method not found"},
		}
	}
}

func handleToolCall(name string, args json.RawMessage) callToolResult {
	switch name {
	case "ReadGo":
		return handleReadGo(args)
	case "UpdateGo":
		return handleUpdateGo(args)
	default:
		return errorResult("unknown tool: " + name)
	}
}

func resolveAndValidatePath(filePath string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("file_path is required")
	}

	resolved, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	// Evaluate symlinks to prevent escaping via symlink
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", fmt.Errorf("failed to resolve working directory: %w", err)
	}

	if !strings.HasPrefix(resolved, cwd+string(filepath.Separator)) && resolved != cwd {
		return "", fmt.Errorf("file_path must be within the working directory (%s)", cwd)
	}

	if filepath.Ext(resolved) != ".go" {
		return "", fmt.Errorf("only works on .go files")
	}

	return resolved, nil
}

func errorResult(msg string) callToolResult {
	return callToolResult{
		Content: []textContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}

func successResult(msg string) callToolResult {
	return callToolResult{
		Content: []textContent{{Type: "text", Text: msg}},
	}
}

// ReadGo implementation

func handleReadGo(rawArgs json.RawMessage) callToolResult {
	var args readGoArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}

	resolved, err := resolveAndValidatePath(args.FilePath)
	if err != nil {
		return errorResult(err.Error())
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return errorResult(err.Error())
	}

	lines := strings.Split(string(data), "\n")

	// Handle offset (1-based)
	start := 0
	if args.Offset > 0 {
		start = args.Offset - 1
	}
	if start > len(lines) {
		start = len(lines)
	}

	end := len(lines)
	if args.Limit > 0 && start+args.Limit < end {
		end = start + args.Limit
	}

	var buf strings.Builder
	for i := start; i < end; i++ {
		// Expand tabs to 4 spaces
		expanded := strings.ReplaceAll(lines[i], "\t", "    ")
		fmt.Fprintf(&buf, "%6d\t%s\n", i+1, expanded)
	}

	return successResult(buf.String())
}

// UpdateGo implementation

func handleUpdateGo(rawArgs json.RawMessage) callToolResult {
	var args updateGoArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}

	resolved, err := resolveAndValidatePath(args.FilePath)
	if err != nil {
		return errorResult(err.Error())
	}
	if args.OldString == "" {
		return errorResult("old_string is required")
	}
	if args.NewString == "" {
		return errorResult("new_string is required")
	}

	// Check if old and new are the same after normalization
	if normalizeBlock(args.OldString) == normalizeBlock(args.NewString) {
		return errorResult("old_string and new_string are identical after whitespace normalization")
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return errorResult(err.Error())
	}

	originalData := data
	fileLines := strings.Split(string(data), "\n")

	// Strip each line of old_string
	oldLines := splitAndStrip(args.OldString)
	if len(oldLines) == 0 {
		return errorResult("old_string is empty after stripping")
	}

	// Find matches
	matches := findMatches(fileLines, oldLines)

	if !args.ReplaceAll {
		if len(matches) == 0 {
			return errorResult(fmt.Sprintf("no match found (first line to match: %q)", oldLines[0]))
		}
		if len(matches) > 1 {
			return errorResult(fmt.Sprintf("%d matches found, provide more context to disambiguate", len(matches)))
		}
		matches = matches[:1]
	} else {
		if len(matches) == 0 {
			return errorResult(fmt.Sprintf("no match found (first line to match: %q)", oldLines[0]))
		}
	}

	// Apply replacements in reverse order to preserve indices
	newLines := splitAndStrip(args.NewString)
	newRawLines := strings.Split(strings.TrimRight(args.NewString, "\n"), "\n")

	for i := len(matches) - 1; i >= 0; i-- {
		matchIdx := matches[i]
		// Determine base indentation from the first matched line in the original file
		baseIndent := getLeadingWhitespace(fileLines[matchIdx])

		// Determine the relative indent of the first old line (in spaces)
		oldRawLines := strings.Split(strings.TrimRight(args.OldString, "\n"), "\n")
		oldBaseSpaces := countLeadingSpaces(oldRawLines[0])

		// Build replacement lines
		var replacementLines []string
		for j, stripped := range newLines {
			if stripped == "" && strings.TrimSpace(newRawLines[j]) == "" {
				replacementLines = append(replacementLines, "")
				continue
			}
			// Calculate relative indent from new_string
			newLineSpaces := countLeadingSpaces(newRawLines[j])
			relativeSpaces := newLineSpaces - oldBaseSpaces
			if relativeSpaces < 0 {
				relativeSpaces = 0
			}
			// Convert relative spaces to tabs (4 spaces = 1 tab)
			extraTabs := relativeSpaces / 4
			line := baseIndent + strings.Repeat("\t", extraTabs) + stripped
			replacementLines = append(replacementLines, line)
		}

		// Replace in fileLines
		after := append([]string{}, fileLines[matchIdx+len(oldLines):]...)
		fileLines = append(fileLines[:matchIdx], append(replacementLines, after...)...)
	}

	// Reconstruct file
	result := strings.Join(fileLines, "\n")

	// Write file
	if err := os.WriteFile(resolved, []byte(result), 0644); err != nil {
		return errorResult("failed to write file: " + err.Error())
	}

	// Run gofmt
	cmd := exec.Command("gofmt", "-w", resolved)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Revert
		os.WriteFile(resolved, originalData, 0644)
		return errorResult("gofmt failed (reverted): " + stderr.String())
	}

	return successResult(fmt.Sprintf("Successfully replaced %d occurrence(s) in %s", len(matches), resolved))
}

func splitAndStrip(s string) []string {
	raw := strings.Split(strings.TrimRight(s, "\n"), "\n")
	stripped := make([]string, len(raw))
	for i, line := range raw {
		stripped[i] = strings.TrimSpace(line)
	}
	return stripped
}

func normalizeBlock(s string) string {
	lines := splitAndStrip(s)
	return strings.Join(lines, "\n")
}

func findMatches(fileLines, oldLines []string) []int {
	var matches []int
	for i := 0; i <= len(fileLines)-len(oldLines); i++ {
		if matchAt(fileLines, oldLines, i) {
			matches = append(matches, i)
		}
	}
	return matches
}

func matchAt(fileLines, oldLines []string, pos int) bool {
	for j, oldLine := range oldLines {
		if strings.TrimSpace(fileLines[pos+j]) != oldLine {
			return false
		}
	}
	return true
}

func getLeadingWhitespace(s string) string {
	trimmed := strings.TrimLeft(s, " \t")
	return s[:len(s)-len(trimmed)]
}

func countLeadingSpaces(s string) int {
	count := 0
	for _, ch := range s {
		if ch == ' ' {
			count++
		} else if ch == '\t' {
			count += 4
		} else {
			break
		}
	}
	return count
}
