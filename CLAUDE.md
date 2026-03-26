# GoEditMCP — Whitespace-Normalized Edit Tool for Go Files

MCP server (stdio) that provides two tools: `ReadGo` and `UpdateGo`. Built to solve the #1 cause of Edit tool failures in Go codebases: tab/space mismatches between what Read shows and what Edit expects.

## Tools

### ReadGo

Reads a Go file and returns contents with tabs expanded to spaces (4 spaces per tab). Line numbers included in output just like the built-in Read tool (`cat -n` style). This ensures the text you see is exactly what you can paste into `UpdateGo` for matching.

Parameters:
- `file_path` (string, required) — absolute path to the Go file
- `offset` (int, optional) — 1-based line number to start reading from
- `limit` (int, optional) — number of lines to read

### UpdateGo

Performs search-and-replace on a Go file using whitespace-normalized matching. Both `old_string` and `new_string` have all leading/trailing whitespace stripped from each line before matching. The replacement preserves the original file's indentation structure.

Parameters:
- `file_path` (string, required) — absolute path to the Go file
- `old_string` (string, required) — text to find (whitespace-normalized for matching)
- `new_string` (string, required) — replacement text (will be re-indented to match the original)
- `replace_all` (bool, optional, default false) — replace all occurrences

#### Matching algorithm

1. Split `old_string` into lines, strip leading/trailing whitespace from each line
2. Split file into lines, for each position try matching stripped lines against stripped old_string lines
3. If exactly one match found, proceed. If zero or multiple matches, return error with context.
4. Determine the indentation of the first matched line in the original file (the leading whitespace characters)
5. For `new_string`: take each line, strip its leading whitespace, then re-indent using the original file's indent style (tabs for Go) at the appropriate depth
6. The indent depth for each new line is determined by: `original_base_indent + (new_line_relative_indent - old_line_relative_indent)`. Relative indent is measured in the normalized (spaces) version.
7. Write the result. Run `gofmt` on the file after writing. If `gofmt` fails, revert and return the gofmt error.

#### Re-indentation detail

The key insight: the agent sends spaces (because that's what ReadGo showed), but Go files use tabs. UpdateGo bridges this by:
- Matching is done on stripped content (whitespace-insensitive)
- The replacement text gets the correct tab-based indentation inferred from context
- Relative indentation within the new_string block is preserved (if line 2 is indented 4 more spaces than line 1 in new_string, it gets one more tab in the output)

## Implementation

- Language: Go (single binary, no dependencies beyond stdlib)
- Protocol: MCP over stdio (JSON-RPC 2.0)
- Must handle the MCP initialize/initialized handshake
- Tool definitions returned via `tools/list`
- Tool execution via `tools/call`
- No external dependencies — just `os`, `strings`, `bytes`, `os/exec` (for gofmt), `encoding/json`, `bufio`

## Build & Run

```bash
cd GoEditMCP
go build -o go-edit-mcp .
```

Configure in Claude Code settings:
```json
{
  "mcpServers": {
    "go-edit": {
      "command": "/path/to/go-edit-mcp"
    }
  }
}
```

## Edge Cases

- If the file doesn't end with a newline, preserve that behavior
- If `old_string` matches zero times, return error: "no match found" with the first stripped line attempted
- If `old_string` matches multiple times, return error: "N matches found, provide more context to disambiguate"
- If `gofmt` fails after replacement, revert the file to its original content and return the gofmt error message so the agent can fix the replacement
- Empty `old_string` or `new_string` should error
- Non-Go files should error ("UpdateGo only works on .go files")
- `old_string` equal to `new_string` (after normalization) should error

## Testing

No test framework needed. Verify manually:
1. Create a Go file with tab indentation
2. ReadGo returns spaces
3. UpdateGo with space-indented old/new strings correctly matches and replaces with tabs
4. gofmt passes after replacement
