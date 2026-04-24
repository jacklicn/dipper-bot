# dipper-bot user manual

**Language:** This is the **English** edition. For the full Chinese manual, see [MANUAL_CN.md](./MANUAL_CN.md). For every `config.json` field, see [CONFIG.md](./CONFIG.md) (English summary) or [CONFIG_CN.md](./CONFIG_CN.md) (complete Chinese). Build & repo layout: [BUILD.md](./BUILD.md) / [BUILD_CN.md](./BUILD_CN.md).

**dipper-bot** is a lightweight personal AI assistant: OpenAI-compatible APIs, channels, cron, MCP tools, and more.

**Interactive CLI:** line editing (arrows, Ctrl+U/K), history with up/down. **Run cancellation:** a new message for the same session aborts the previous run; optional per-run timeout (`runTimeoutSec`).

**No-args / double-click:** starts an interactive REPL: type `agent`, `status`, etc.; `help` or `?` prints help; `exit` / `quit` / `:q` exits.

---

## 1. Quick start

### 1.1 Initialize

**macOS / Linux**

```bash
./dipper-bot onboard
```

**Windows**

```powershell
.\dipper-bot.exe onboard
```

You will be prompted for a workspace path (default `~/.dipper-bot/workspace`). The path is stored in `~/.dipper-bot/config.json`; full settings live in `<workspace>/config.json`.

### 1.2 API key

Edit `workspace/config.json`:

```json
{
  "providers": {
    "custom": {
      "apiKey": "your-api-key",
      "apiBase": "https://api.openai.com/v1"
    }
  }
}
```

### 1.3 Chat

```bash
./dipper-bot agent -m "Hello!"
./dipper-bot agent              # interactive
./dipper-bot agent --web        # browser UI, default http://localhost:8600
```

---

## 2. Commands (overview)

| Command | Purpose |
|---------|---------|
| *(no args)* | Interactive REPL |
| `help` / `-h` / `--help` | Usage |
| `onboard [--workspace PATH]` | Create config + workspace |
| `agent [-m MSG] [--web] …` | Run the agent (see `dipper-bot agent -h`) |
| `gateway [-p PORT]` | HTTP gateway (default **8090**) |
| `status` | Config path, workspace, model, key status |
| `channels status` / `channels login` | Channel state / WhatsApp QR flow |
| `cron list|add|remove|run|enable` | Scheduled jobs (per workspace) |
| `provider login openai-codex` / `github-copilot` | OAuth helpers |

**Platform:** macOS/Linux `./dipper-bot`; Windows `.\dipper-bot.exe`. Many commands accept `--workspace` to override the workspace.

---

## 3. Configuration (overview)

- **Default pointer:** `~/.dipper-bot/config.json` — mainly `agents.defaults.workspace`.
- **Full config:** `<workspace>/config.json` overrides defaults for that workspace.

**Providers**

- **custom** — OpenAI-compatible `apiKey` + optional `apiBase`.
- **openai_codex** / **github_copilot** — OAuth; set `model` accordingly, then run `provider login …`.

**Memory & context (high level)**

- **`contextWindowTokens` > 0** — token-based consolidation into `memory/MEMORY.md` + `memory/HISTORY.md`.
- **`lcm.enabled`** — lossless-style DAG + SQLite; tools `lcm_grep`, `lcm_describe`, etc. See [LCM.md](./LCM.md).

**Experience / usage (optional)**

- `agents.defaults.experience` — fenced memory recall, nudges, learner feedback mode (`learnerFeedbackInstantPush`, default true for immediate async learner bubbles; false for next-reply digest), usage JSONL, `usageCostCurrency` (CNY/USD), `defaultUsdToCny`, pricing overrides. See [CONFIG.md](./CONFIG.md).

Full parameter list, defaults, and channel blocks: **[CONFIG.md](./CONFIG.md)** (English) or **[CONFIG_CN.md](./CONFIG_CN.md)** (Chinese, longest).

- **`agents.defaults.attachments.maxEmbeddedBytes`** — optional cap for inline PDF/Office Markdown expanded from `[附件] uploads/…` lines before the model call (`0` = tool default; see CONFIG.md).

---

## 4. Web UI (`agent --web`)

- Open `http://localhost:8600` (unless `--host` / `-p` changed).
- **POST /api/chat** — `{"content":"…"}` → assistant reply or `error`.
- **GET /api/learning/dashboard** — learning telemetry dashboard JSON (`window_hours` optional; used by the `Learning` drawer in web chat).
- **POST /api/upload** — multipart `file` → paths under `uploads/`.
- **POST /api/transcribe** — multipart `audio` (requires transcription config).

Slash: `/new`, `/help`. Session id defaults to `web:default`.

---

## 5. Gateway API

With `dipper-bot gateway` running:

**POST /message** — enqueue inbound traffic (e.g. channel integrations). Example body:

```json
{ "channel": "web", "chat_id": "user1", "sender_id": "user", "content": "Hello" }
```

Replies go out via the outbound bus. **Same session:** new message cancels the in-flight run; optional `runTimeoutSec` in config.

---

## 6. Channels (summary)

Enable under `channels.*` in `workspace/config.json`. Typical needs:

| Channel | Notes |
|---------|--------|
| **Telegram** | Bot token from @BotFather |
| **WhatsApp** | `channels login`; Node bridge under `bridge/`; per-workspace auth dir |
| **Discord** | Bot token; optional allowlists |
| **Feishu / DingTalk / Slack / Email / QQ / Wecom / Webhook** | See [CONFIG_CN.md](./CONFIG_CN.md) §5 for field-level detail (Chinese) or [CONFIG.md](./CONFIG.md) for English summaries |

Multi-workspace: separate `config.json`, cron files, WhatsApp auth, and often different `bridgeUrl` / gateway ports.

---

## 7. Workspace layout (typical)

```
workspace/
  config.json
  memory/           USER.md, NOTE.md, usage_events.jsonl, …
  sessions/         JSONL per session
  cron/jobs.json
  uploads/
```

---

## 8. Tools (agent)

Includes: **read/write/edit_file**, **list_dir**, **exec**, **message**, **cron/spawn**, **web_search / web_fetch**, **memory**, **session_search**, **lcm_***, **MCP** tools from `tools.mcpServers`, **sessions_***, **usage_insights**, and more. Restrict workspace writes with `tools.restrictToWorkspace` where applicable.

### 8.1 Chrome DevTools MCP (default-enabled config)

The default generated `workspace/config.json` already includes:

```json
"tools": {
  "mcpServers": {
    "chrome-devtools": {
      "command": "npx",
      "args": [
        "-y",
        "chrome-devtools-mcp@latest",
        "--no-performance-crux",
          "--no-usage-statistics"
      ]
    }
  }
}
```

By default no attach mode is preset. Add **`--browser-url=http://127.0.0.1:9222`** (adjust as needed), or use **`--auto-connect`** (Chrome M144+ remote debugging at `chrome://inspect/#remote-debugging`).

### 8.4 PDF / Word / Excel / PPT → Markdown（供大模型分析）

对 **PDF、Word（.docx）、Excel（.xlsx/.xls）、PowerPoint（.pptx）** 做摘要、问答或结构化分析时，宜先在**工作区根目录**使用 Python 虚拟环境安装 [MarkItDown](https://github.com/microsoft/markitdown)，将文档**稳定转为 Markdown**，再 `read_file` 或将正文送入模型；避免依赖不稳定的直接二进制读取。

1. 在工作区（`agents.defaults.workspace`）创建并激活 `.venv`（Python 3.10+）。
2. 执行一次：`python -m pip install 'markitdown[all]'`（或按需 `'markitdown[pdf,docx,pptx,xlsx,xls]'`）。
3. 转换示例：`markitdown 文件.pdf -o 文件.md`（或 `python -m markitdown ...`）。

`exec` 在工作区内执行 `python`/`python3` 时会**自动使用** `workspace/.venv`（不存在时会尝试创建空 venv），仍需在 venv 内安装 MarkItDown。详细步骤与注意事项见内置技能 **`markitdown`**（`workspace/skills/markitdown/SKILL.md`）。

---

## 9. FAQ (short)

- **Where is my API key?** — `workspace/config.json` → `providers.custom.apiKey` (or OAuth paths for Codex/Copilot).
- **Two configs?** — `~/.dipper-bot/config.json` points at the workspace; real knobs are in the workspace `config.json`.
- **Emoji on Windows cmd?** — Use Windows Terminal; UTF-8 code page.

---

## 10. Version

See repository tags and `CHANGELOG` if present. For exhaustive CLI flags and channel walkthroughs, prefer **MANUAL_CN** until this English file grows.
