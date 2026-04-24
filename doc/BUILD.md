# Building & development

How to compile **dipper-bot** from source, run tests, and navigate the repository. For day-to-day commands and the interactive CLI, see [MANUAL.md](./MANUAL.md). For every `config.json` field, see [CONFIG.md](./CONFIG.md). **Chinese:** [MANUAL_CN.md](./MANUAL_CN.md), [CONFIG_CN.md](./CONFIG_CN.md), [BUILD_CN.md](./BUILD_CN.md).

## Requirements

- **Go 1.26** (or adjust `go.mod` if you target an older toolchain)

## Build

**macOS / Linux:**

```bash
cd dipper-bot
go build -o dipper-bot .
# optional: make
```

**Windows (PowerShell):**

```powershell
cd dipper-bot
go build -o dipper-bot.exe .
```

First-time setup:

```bash
./dipper-bot onboard          # creates ~/.dipper-bot/config.json + workspace
# add providers.custom.apiKey in workspace/config.json
./dipper-bot agent -m "Hello!"
```

Use `.\dipper-bot.exe` on Windows. Double-clicking the `.exe` opens the interactive REPL (UTF-8; use [Windows Terminal](https://learn.microsoft.com/windows/terminal/install) if emoji render incorrectly).

## Tests

```bash
go test ./...
```

Live web integration tests are opt-in (network-dependent):

```bash
DIPPER_ENABLE_LIVE_WEB_TESTS=1 go test ./tools -run TestWebSearchTool_DuckDuckGo_live
```

## Project layout

| Path | Role |
|------|------|
| `main.go` | CLI entrypoint |
| `agent/` | Context builder, loop, memory, skills, adaptive controller |
| `tools/` | Files, exec, message, cron, spawn, web, memory/consolidator, LCM tools, MCP |
| `lcm/` | Lossless context (SQLite, DAG summarization) |
| `bus/` | Inbound/outbound message bus |
| `config/` | Schema, loader, migrations |
| `gateway/` | HTTP server (`POST /message`, etc.) |
| `web/` | Web chat UI for `agent --web` |
| `cron/` | Scheduled jobs (`workspace/cron/jobs.json`) |
| `session/` | Session manager (JSONL) |
| `channels/` | Telegram, WhatsApp, Discord, Feishu, DingTalk, Slack, Email, QQ, Wecom, Webhook |
| `bridge/` | WhatsApp bridge (Node.js / Baileys) |
| `deploy/` | Docker Compose, systemd, Windows service examples |
| `heartbeat/` | Periodic `HEARTBEAT.md` check |
| `providers/` | OpenAI-compatible HTTP client |

## Deployment

- **Docker**: `docker compose up -d dipper-bot-gateway` (see `deploy/`)
- **Linux (systemd)** / **Windows (NSSM, sc.exe)**: see [deploy/README.md](../deploy/README.md)

## Related docs

- [MANUAL.md](./MANUAL.md) — operations, CLI, channels
- [CONFIG.md](./CONFIG.md) — configuration reference
- [DESIGN.md](./DESIGN.md) — architecture
- [LCM.md](./LCM.md) — lossless context (LCM)
- [PRIVACY.md](./PRIVACY.md) — privacy & data flow
- Chinese editions: `*_CN.md` in this directory
