# dipper-bot configuration reference

**Language:** This is the **English** edition (condensed). The **complete** field-by-field guide with long examples is **[CONFIG_CN.md](./CONFIG_CN.md)** (Chinese). Operations: [MANUAL.md](./MANUAL.md) / [MANUAL_CN.md](./MANUAL_CN.md).

## File locations

| File | Role |
|------|------|
| `~/.dipper-bot/config.json` | Stores the **workspace path** (`agents.defaults.workspace`) and minimal defaults |
| `<workspace>/config.json` | **Main** configuration for providers, tools, channels, gateway |

**Priority:** workspace `config.json` wins over `~/.dipper-bot/config.json` for merged keys.

---

## Top-level shape

```text
agents.defaults    → model, workspace, memory, LCM, experience, attachments, timeouts, …
providers          → custom, groq, transcription, codex/copilot helpers
gateway            → host, port
tools              → web, exec, mcpServers, restrictToWorkspace, …
channels           → telegram, whatsapp, discord, feishu, …
logging            → optional rotating file logs under `<workspace>/<dir>/`
```

---

## `logging` (process-wide slog)

Rotating files live under the **workspace** (default `<workspace>/logs/dipper-bot.log`). Used by `dipper-bot agent`, `gateway`, and `cron` after config load.

| Key | Type | Default | Notes |
|-----|------|---------|--------|
| `enabled` | bool | **true** | Set `false` to disable log files (stderr only). |
| `level` | string | `info` | `debug`, `info`, `warn`, `error`. Gateway `--verbose` forces `debug`. |
| `maxAgeDays` | int | **7** | Delete rotated files older than this (lumberjack `MaxAge`). |
| `maxSizeMB` | int | **128** | Rotate when the active file exceeds this size (MiB). |
| `maxBackups` | int | `0` | Optional cap on rotated file count; `0` = prune by age only. |
| `fileOnly` | bool | `false` | When `true`, write **only** to the file (no stderr copy). |
| `dir` | string | `logs` | Subdirectory under workspace. |
| `filename` | string | `dipper-bot.log` | Base name only (path segments are stripped for safety). |
| `compress` | bool | `false` | Gzip old rotated logs. |

Full detail: **CONFIG_CN.md** (search for `logging`).

---

## `agents.defaults` (essentials)

| Key | Type | Notes |
|-----|------|--------|
| `workspace` | string | Data root (`~` expanded). Default `~/.dipper-bot/workspace`. |
| `model` | string | e.g. `gpt-4o`, `anthropic/claude-…`, `openai-codex/…`, `github-copilot/…`. |
| `maxTokens` | int | Max completion tokens (default ~8192). |
| `temperature` | float | Sampling temperature (default ~0.7). |
| `maxToolIterations` | int | Max tool rounds per user turn (default 20). |
| `memoryWindow` | int | Recent turns when LCM + token memory are off (default 50). |
| `contextWindowTokens` | int | When **> 0**, MemoryConsolidator archives to `MEMORY.md` / `HISTORY.md` by token budget. |
| `reasoningEffort` | string | Optional `low` / `medium` / `high` for thinking-style models. |
| `runTimeoutSec` | int | Per-run wall timeout; `0` = none. |
| `lcm` | object | Lossless context — see [LCM.md](./LCM.md). |
| `memoryMaintenance` | object | Autonomous memory-maintainer loop (quality/confidence thresholds, adaptive controller, semantic regroup caps). |
| `skillsEvolution` | object | Autonomous skill-maintainer loop (creation cadence, min tool calls, adaptive controller). |
| `experience` | object | UX flags + usage accounting: fenced recall, **memoryPromptNudgeEvery** / **skillPromptNudgeEvery** (defaults merged to ~10 when omitted; `0` disables), **learnerFeedbackInstantPush** (default **on**: immediate second outbound for async learner writes; set `false` to use next-reply digest only), compaction logs, usage JSONL, **usageCostCurrency** (`CNY`/`USD`), **defaultUsdToCny**, `usagePricingOverrides`. |
| `attachments` | object | Optional `maxEmbeddedBytes` cap for PDF/Office text inlined from `[附件] …` lines before the model sees the message. |

### `lcm` (short)

| Key | Meaning |
|-----|---------|
| `enabled` | LCM on (**default on** when omitted; set `false` to disable) |
| `databasePath` | Optional SQLite path (default under workspace) |
| `contextThreshold` | When estimated fill exceeds this ratio, consolidate |
| `freshTailCount` | Recent raw messages kept |
| `leafMinFanout`, `condensedMinFanout` | Fan-out for leaf / condensed nodes |
| `leafChunkTokens`, `leafTargetTokens`, `condensedTargetTokens` | Chunk sizing |
| `incrementalMaxDepth` | DAG depth cap for incremental summaries |

Full numeric guidance: **CONFIG_CN** §1 (Agent).

### `memoryMaintenance` / `skillsEvolution` (short)

Both loops are first-class runtime features (enabled by default in `DefaultConfig()`), and can be turned off per workspace.

- `memoryMaintenance`: maintains `memory/USER.md` and `memory/NOTE.md` through constrained add/replace/remove decisions.
- `skillsEvolution`: proposes/patches workspace skills via `skill_manage` when repeated tool workflows are detected; optional **mid-run** reflects (`midRunReflectEveryToolIters`, `midRunReflectMinSeconds`) run extra skill passes between tool rounds on long turns. Successful autonomous writes append a short **skill update** note to the main reply (mid-run). For async worker/flush writes, default mode sends immediate outbound bubbles; set `experience.learnerFeedbackInstantPush=false` to switch to next-reply digest across all channels (mutually exclusive).
- Shared tuning concepts: `minQualityScore`, `minConfidence`, suppression/backoff windows, and PID-style controller fields (`controller*`).

---

## `providers`

### `providers.custom`

OpenAI-compatible HTTP:

- `apiKey` (required for custom models)
- `apiBase` (default `https://api.openai.com/v1`)
- `extraHeaders` (optional map)

### OAuth flows

Use CLI `dipper-bot provider login openai-codex` / `github-copilot` and set `model` to the matching prefix. Tokens live in documented paths (see MANUAL / CONFIG_CN).

### `providers.groq` / `providers.transcription`

Optional Groq key; transcription `provider`: `groq` or `vosk` + `voskUrl` for local WS.

---

## `gateway`

- `host` — bind address (e.g. `0.0.0.0`)
- `port` — default **8090**
- `rateLimitPerMinute` — request budget per minute (default 120)
- `rateLimitIPv4Prefix` / `rateLimitIPv6Prefix` — IP aggregation prefix lengths (defaults `/32` and `/128`)
- `rateLimitCidrs` — optional explicit CIDR buckets

---

## `tools`

### `tools.web.search`

- `provider`: `duckduckgo` (default), `brave`, `tavily`, `jina`, `searxng`
- `apiKey` / `baseUrl` as required by provider
- Env fallbacks: `BRAVE_API_KEY`, `TAVILY_API_KEY`, `JINA_API_KEY`, `SEARXNG_BASE_URL`

### `tools.web.proxy`

HTTP/SOCKS proxy string for `web_search` / `web_fetch`.

### `tools.exec`

- `timeout` — seconds for shell/exec tool

### Browser automation (MCP)

Use `tools.mcpServers` with the official [chrome-devtools-mcp](https://www.npmjs.com/package/chrome-devtools-mcp) package ([GitHub](https://github.com/ChromeDevTools/chrome-devtools-mcp)). **Requirements** (per npm): Node.js **v20.19+** (or a newer maintenance LTS), **npm**, and **current stable Chrome or newer**; only **Chrome / [Chrome for Testing](https://developer.chrome.com/blog/chrome-for-testing/)** is officially supported.

> Connecting the MCP client to the server **does not** start a browser by itself; the server launches or attaches when a tool first needs a browser instance.

**`--autoConnect`**: Chrome **M144+** must have remote debugging enabled at `chrome://inspect/#remote-debugging` (Edge can use `edge://inspect/#remote-debugging`) so `--autoConnect` can match a local running instance for the profile implied by `--channel` (default `stable`).

**Common CLI flags** (pass in `mcpServers.<name>.args`; full list: `npx chrome-devtools-mcp@latest --help` and npm **Configuration**):

| Flag | Notes |
|------|--------|
| `--autoConnect` / `--auto-connect` | Attach to a local running Chrome (M144+, remote debugging on). Default `false`. |
| `--browserUrl` / `--browser-url` / `-u` | Attach to a debuggable instance, e.g. `http://127.0.0.1:9222`. |
| `--wsEndpoint` / `--ws-endpoint` / `-w` | WebSocket debugger URL; alternative to `--browserUrl`. Read `webSocketDebuggerUrl` from `http://127.0.0.1:9222/json/version`. |
| `--wsHeaders` / `--ws-headers` | Custom WebSocket headers (JSON string); only with `--wsEndpoint`. |
| `--headless` | Headless mode; default `false`. |
| `--executablePath` / `-e` | Path to a custom Chrome binary. |
| `--isolated` | Temporary user-data-dir, cleaned up after the browser exits. |
| `--userDataDir` / `--user-data-dir` | Chrome profile path; see upstream defaults if omitted. |
| `--channel` | `stable` (default), `beta`, `dev`, `canary`. |
| `--slim` | Smaller tool surface (navigation, script eval, screenshots, etc.). |
| `--viewport` | Initial viewport, e.g. `1280x720`; headless max 3840×2160. |
| `--proxyServer` / `--proxy-server` | Proxy passed through to Chrome. |
| `--acceptInsecureCerts` | Ignore self-signed / expired certs (use with care). |
| `--chromeArg` / `--chrome-arg` | Extra Chrome flags when the MCP server launches Chrome. |
| `--categoryEmulation` / `--categoryPerformance` / `--categoryNetwork` / `--categoryExtensions` | Set `false` (or enable extensions with `true`) to trim tool groups; see upstream for version / connection caveats. |
| `--performanceCrux` | Set `false` or pass **`--no-performance-crux`**: do not send trace URLs to CrUX. |
| `--usageStatistics` | Set `false` or pass **`--no-usage-statistics`**: opt out of usage statistics. |
| `--logFile` / `--log-file` | Debug log path; set `DEBUG=*` for verbose logs. |

**Privacy / updates**: statistics are on by default; use **`--no-usage-statistics`**, or set **`CHROME_DEVTOOLS_MCP_NO_USAGE_STATISTICS`** / **`CI`** in `mcpServers.<name>.env`. Periodic npm update notices can be disabled with **`CHROME_DEVTOOLS_MCP_NO_UPDATE_CHECKS`**.

**Experimental** (see npm): `--experimentalVision`, `--experimentalScreencast` ([ffmpeg](https://www.ffmpeg.org/download.html) on `PATH`), `--experimentalWebmcp` (Chrome 149+ with specific feature flags), etc.

More: [troubleshooting](https://github.com/ChromeDevTools/chrome-devtools-mcp/blob/main/docs/troubleshooting.md), [tool reference](https://github.com/ChromeDevTools/chrome-devtools-mcp/blob/main/docs/tool-reference.md).

**Examples** (`<workspace>/config.json`, under `tools`):

**Built-in default** (`config.DefaultConfig()`): no explicit browser attach flags; only no CrUX / no usage statistics:

```json
{
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
}
```

**Fixed CDP URL** (`--browser-url`, change host/port as needed):

```json
{
  "tools": {
    "mcpServers": {
      "chrome-devtools": {
        "command": "npx",
        "args": [
          "-y",
          "chrome-devtools-mcp@latest",
          "--no-performance-crux",
          "--no-usage-statistics",
          "--browser-url=http://127.0.0.1:9222"
        ]
      }
    }
  }
}
```

Lightweight headless (`--slim` + `--headless`):

```json
{
  "tools": {
    "mcpServers": {
      "chrome-devtools": {
        "command": "npx",
        "args": ["-y", "chrome-devtools-mcp@latest", "--no-performance-crux", "--no-usage-statistics", "--slim", "--headless=true"]
      }
    }
  }
}
```

The built-in default does **not** include `--browser-url` or `--auto-connect`. Choose one attach mode in `args` as needed: **`--browser-url`**, **`--auto-connect`** (with remote debugging), or **`--ws-endpoint`**. See [Connecting to a running Chrome instance](https://github.com/ChromeDevTools/chrome-devtools-mcp#connecting-to-a-running-chrome-instance).

### `tools.restrictToWorkspace`

When true, constrain file/exec paths to the workspace (recommended). **Default is `true`**; omit the field or set `false` only if you need unrestricted paths.

### `tools.mcpServers`

Map of server name → `{ command, args?, env?, url?, headers?, toolTimeout?, enabledTools? }`  
stdio (`command`) or HTTP (`url`). MCP tools are registered with the agent when configured.

---

## `channels` (per integration)

Each channel has `enabled` plus provider-specific fields (`token`, `appId`, `bridgeUrl`, `allowFrom`, …). **CONFIG_CN §5** lists every field for Telegram, WhatsApp, Discord, Feishu, DingTalk, Slack, Email, QQ, Wecom, Webhook, etc.

For `channels.webhook`, rate-limit fields mirror `gateway`: `rateLimitPerMinute`, `rateLimitIPv4Prefix`, `rateLimitIPv6Prefix`, `rateLimitCidrs`.

**Multi-instance:** cron lives in `workspace/cron/jobs.json`; WhatsApp auth in `workspace/whatsapp-auth`; use distinct tokens / bridge ports per workspace.

---

## Minimal example

```json
{
  "agents": {
    "defaults": {
      "workspace": "~/.dipper-bot/workspace",
      "model": "gpt-4o",
      "maxTokens": 8192,
      "maxToolIterations": 20,
      "temperature": 0.7
    }
  },
  "providers": {
    "custom": {
      "apiKey": "sk-…",
      "apiBase": "https://api.openai.com/v1"
    }
  }
}
```

---

## Workspace vs token budget

Large files under `memory/` and skills can increase prompt size. Use `contextWindowTokens` + consolidator or LCM to cap growth. Details: **CONFIG_CN** §7–8.

---

## FAQ (config)

- **Why two JSON files?** — Home file is a pointer; workspace file is the real source of truth for a deployment.
- **MCP not loading?** — Check `command`/`url`, timeouts, and that the server binary exists on `PATH`.

For exhaustive tables (every optional key, Chinese prose), open **[CONFIG_CN.md](./CONFIG_CN.md)**.
