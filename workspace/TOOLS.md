# Available Tools

This document describes the tools available to dipper-bot.

## File Operations

### read_file
Read the contents of a file. Paths are relative to workspace. Web-uploaded files go to `uploads/`; filenames with quotes or special chars are sanitized (e.g. `"` `「」` `『』` → `_`). Supports both ASCII and Chinese quotation marks. If a path fails, the tool will retry with the sanitized filename.

**PDF and Office** (`.pdf`, `.doc`/`.docx`, `.xls`/`.xlsx`, `.ppt`/`.pptx`): the tool converts the file to **Markdown** with MarkItDown using `workspace/.venv` (not raw binary). Install once: `exec(command="python -m pip install 'markitdown[all]'")`.
```
read_file(path: str) -> str
```

### write_file
Write content to a file (creates parent directories if needed). For **Word / Excel / PowerPoint / PDF / images** and other binary, set **`content_encoding` to `"base64"`** and put **standard Base64** in `content` (JSON cannot carry raw binary; writing UTF-8 mojibake corrupts Office ZIP files). For text files, omit `content_encoding` or use `"text"`. Prefer **`exec`** + Python (`python-docx`, `openpyxl`, `python-pptx`, etc.) writing with `open(path, "wb")` when generating Office files.
```
write_file(path: str, content: str, content_encoding: str = "text") -> str
```

### edit_file
Edit a file by replacing specific text.
```
edit_file(path: str, old_text: str, new_text: str) -> str
```

### list_dir
List contents of a directory.
```
list_dir(path: str) -> str
```

## Python Scripts

Create Python scripts in the workspace and run them with the built-in venv:

1. **Write script** — `write_file(path="script.py", content="...")` — paths are relative to workspace.
2. **Run script** — `exec(command="python script.py")` — automatically uses `workspace/.venv` (created on first use).

## Shell Execution

### exec
Execute a shell command and return output. Uses `cmd /c` on Windows, `sh -c` on Unix.
```
exec(command: str, working_dir: str = None) -> str
```

**Safety Notes:**
- Commands have a configurable timeout (default 60s)
- Dangerous commands are blocked (rm -rf, format, dd, shutdown, etc.)
- Output is truncated at 10,000 characters
- `tools.restrictToWorkspace` limits paths to the workspace (**default `true`** in generated config; set `false` only if you need paths outside the workspace)

## Web Access

Only use **`web_search`** and **`web_fetch`** when the user clearly asks to search the web, verify something online, or wants a specific URL read—do not call them by default.

### web_fetch vs real browser (MCP)

- **`web_fetch`** — HTTP GET + readability-style extraction to text/markdown. Good for **static public pages** and “read this URL.” It does **not** open your Chrome window, run JavaScript as a logged-in user, or click UI.
- **`mcp_*` tools** — When `tools.mcpServers` is set (default workspace config often includes **Chrome DevTools MCP**), tools are registered as **`mcp_<serverName>_<toolName>`**. Use these for **operating the real browser**: new tab, navigate, click, type, screenshot, console, pages behind login. Defaults do not preset attach mode; choose **`--browser-url`**, **`--auto-connect`** (Chrome M144+ with remote debugging at `chrome://inspect/#remote-debugging`), or **`--ws-endpoint`** in `args` (see `doc/CONFIG.md`).

If the user asks to “open the browser,” “click,” “take a screenshot of the page,” or use a **logged-in** site, prefer **`mcp_*`** — not `web_fetch`.

### web_search
Search the web. Provider comes from config (`tools.web.search.provider`): default **duckduckgo** needs no API key (uses `ddgsearch`, then `api.duckduckgo.com` if the first path returns no hits). **brave** / others use their respective keys.
```
web_search(query: str, count: int = 5) -> str
```

Returns search results with titles, URLs, and snippets. Brave (and similar providers) require `tools.web.search.apiKey` in config.

### web_fetch
Fetch and extract main content from a URL.
```
web_fetch(url: str, extractMode: str = "markdown", maxChars: int = 50000) -> str
```

**Notes:**
- Content is extracted using readability
- Supports markdown or plain text extraction
- Output is truncated at 50,000 characters by default
- This is **not** a substitute for MCP browser automation (see above)

## MCP tools (dynamic)

MCP servers are configured under **`tools.mcpServers`** in workspace `config.json`. Each tool from a server is exposed as:

```
mcp_<serverName>_<originalToolName>(arguments per tool schema) -> str
```

Examples (exact names depend on the MCP package version):
- **`chrome-devtools`** — defaults in code: `npx -y chrome-devtools-mcp@latest --no-performance-crux --no-usage-statistics` (set `--browser-url`, `--auto-connect`, or `--ws-endpoint` as needed). See `doc/CONFIG.md`, `doc/MANUAL.md` / `*_CN.md`.

If startup cannot connect to an MCP server, those tools will **not** appear in the tool list; check process logs for `MCP connect failed`.

**Duplicate calls in one turn:** If the model emits several identical `mcp_*` tool calls (same name and same JSON arguments) in a single assistant message, dipper-bot **executes the first** and **reuses that result** for the rest (so e.g. two `new_page` with the same URL only open one tab). Non-`mcp_` tools are never deduplicated this way.

## Communication

### message
Send a message to the user (used internally).
```
message(content: str, channel: str = None, chat_id: str = None) -> str
```

## Background Tasks

### spawn
Spawn a subagent to handle a task in the background.
```
spawn(task: str, label: str = None) -> str
```

Use for complex or time-consuming tasks that can run independently. The subagent will complete the task and report back when done. When **`tools.mcpServers`** is configured, the subagent registry also loads the same MCP tools so background tasks can drive the browser when appropriate.

## Scheduled Reminders (Cron)

Use the `exec` tool to create scheduled reminders with `dipper-bot cron add`:

### Set a recurring reminder
```bash
# Every day at 9am
dipper-bot cron add --name "morning" --message "Good morning! ☀️" --cron "0 9 * * *"

# Every 2 hours
dipper-bot cron add --name "water" --message "Drink water! 💧" --every 7200
```

### Set a one-time reminder
```bash
# At a specific time (ISO format)
dipper-bot cron add --name "meeting" --message "Meeting starts now!" --at "2025-01-31T15:00:00"
```

### Manage reminders
```bash
dipper-bot cron list              # List all jobs
dipper-bot cron remove <job_id>   # Remove a job
```

## Heartbeat Task Management

The `HEARTBEAT.md` file in the workspace is checked every 30 minutes.
Use file operations to manage periodic tasks:

### Add a heartbeat task
```python
# Append a new task
edit_file(
    path="HEARTBEAT.md",
    old_text="## Example Tasks",
    new_text="- [ ] New periodic task here\n\n## Example Tasks"
)
```

### Remove a heartbeat task
```python
# Remove a specific task
edit_file(
    path="HEARTBEAT.md",
    old_text="- [ ] Task to remove\n",
    new_text=""
)
```

### Rewrite all tasks
```python
# Replace the entire file
write_file(
    path="HEARTBEAT.md",
    content="# Heartbeat Tasks\n\n- [ ] Task 1\n- [ ] Task 2\n"
)
```

---

## Adding Custom Tools

To add built-in Go tools (not MCP):
1. Implement the `Tool` interface in `dipper-bot/tools/` (`Name`, `Description`, `Parameters`, `Execute`).
2. Register the tool in `agent/loop.go` inside `NewAgentLoop` (same pattern as `read_file`, `web_search`, etc.).

For external capabilities without recompiling, prefer **`tools.mcpServers`** and an MCP server (see `doc/CONFIG.md`).
