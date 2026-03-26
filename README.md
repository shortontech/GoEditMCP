# GoEditMCP

MCP server that provides whitespace-normalized `ReadGo` and `UpdateGo` tools for editing Go files. Solves the #1 cause of Edit tool failures in Go codebases: tab/space mismatches between what Read shows and what Edit expects.

## Tools

### ReadGo

Reads a Go file with tabs expanded to 4 spaces. Returns `cat -n` style output with line numbers, so the text you see is exactly what you can paste into `UpdateGo`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `file_path` | string | yes | Absolute path to the Go file |
| `offset` | int | no | 1-based line number to start reading from |
| `limit` | int | no | Number of lines to read |

### UpdateGo

Search-and-replace on a Go file using whitespace-normalized matching. Both `old_string` and `new_string` have leading/trailing whitespace stripped from each line before matching. The replacement is re-indented with tabs to match the original file. Runs `gofmt` after writing and reverts on failure.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `file_path` | string | yes | Absolute path to the Go file |
| `old_string` | string | yes | Text to find (whitespace-normalized) |
| `new_string` | string | yes | Replacement text (re-indented to match original) |
| `replace_all` | bool | no | Replace all occurrences (default false) |

## Install

```bash
go install github.com/shortontech/GoEditMCP@latest
```

Or build from source:

```bash
git clone https://github.com/shortontech/GoEditMCP.git
cd GoEditMCP
go build -o go-edit-mcp .
```

## Configure

Add to your Claude Code MCP settings (`.mcp.json`):

```json
{
  "mcpServers": {
    "go-edit": {
      "command": "go-edit-mcp"
    }
  }
}
```

If you built from source, use the full path to the binary instead.

## How it works

1. `ReadGo` expands tabs to spaces so you see consistent indentation
2. You copy what `ReadGo` showed you into `UpdateGo`'s `old_string`
3. `UpdateGo` strips whitespace from both the file and your input before matching — so tabs vs spaces doesn't matter
4. The replacement text gets re-indented with the correct tab-based indentation inferred from the match location
5. `gofmt` runs automatically; if it fails, the file is reverted and the error is returned

## License

MIT
