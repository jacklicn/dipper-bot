# dipper-bot

🐕 **dipper-bot** — a compact, self-hosted personal AI assistant written in **Go**: one process runs the gateway, agent loop, tools, and optional web UI. Same session cancels the previous run when a new message arrives; optional per-run timeout.

## Features

- **Gateway + agent** — HTTP `POST /message`, in-process bus, OpenAI-compatible LLM calls (plus optional Codex / GitHub Copilot OAuth flows).
- **Tools** — read/write/edit files, list dir, shell exec, user messaging, cron & spawn, web search & fetch, CDP browser automation, **MCP** servers, session list/history/send, **LCM** tools (`lcm_grep`, `lcm_describe`, …).
- **Context** — optional **token-based memory** (MemoryConsolidator → `MEMORY.md` / `HISTORY.md`); **LCM** (DAG + SQLite, lossless-style recall) **on by default** (`agents.defaults.lcm.enabled` to turn off).
- **Curated memory** — `memory` / `session_search` over `USER.md`, `NOTE.md`, and FTS; background memory maintenance hooks.
- **Learning loops** — autonomous `memoryMaintenance` + `skillsEvolution` (including optional mid-run skill reflection); learner feedback defaults to immediate outbound updates (`experience.learnerFeedbackInstantPush=true`, set false for next-reply digest mode).
- **Channels** — Telegram, WhatsApp (Baileys bridge), Discord, Feishu, DingTalk, Slack, Email, QQ, Wecom, Webhook (see manual).
- **Web chat** — built-in browser chat UI (`agent --web`, default `http://localhost:8600`).
- **Safety guards** — strict JSON decoding + request size limits on gateway/webhook ingress; web fetch blocks private/loopback targets (SSRF protection).
- **Extras** — heartbeat, scheduled cron jobs per workspace.

## Quick start

```bash
go build -o dipper-bot .
./dipper-bot onboard
# set providers.custom.apiKey in workspace/config.json
./dipper-bot agent -m "Hello!"
./dipper-bot agent --web    # recommended: web chat UI in browser (default http://localhost:8600)
./dipper-bot gateway --port 8090
```

On Windows: `go build -o dipper-bot.exe .` then `.\dipper-bot.exe …` (same flags, e.g. `.\dipper-bot.exe agent --web`).

## Documentation

Default editions are **English** (`*.md`). Chinese mirrors: **`*_CN.md`** (same folder). This readme: **[README_CN.md](./README_CN.md)**.

| English | Chinese | Topics |
|---------|---------|--------|
| [README.md](./README.md) | [README_CN.md](./README_CN.md) | Project overview & quick start |
| [doc/MANUAL.md](./doc/MANUAL.md) | [doc/MANUAL_CN.md](./doc/MANUAL_CN.md) | Install, CLI, channels, gateway / Web UI |
| [doc/CONFIG.md](./doc/CONFIG.md) | [doc/CONFIG_CN.md](./doc/CONFIG_CN.md) | `config.json` (EN summary; CN is the full field guide) |
| [doc/BUILD.md](./doc/BUILD.md) | [doc/BUILD_CN.md](./doc/BUILD_CN.md) | Build, tests, repo layout, deploy pointers |
| [doc/DESIGN.md](./doc/DESIGN.md) | [doc/DESIGN_CN.md](./doc/DESIGN_CN.md) | Architecture |
| [doc/LCM.md](./doc/LCM.md) | [doc/LCM_CN.md](./doc/LCM_CN.md) | Lossless context (LCM) |
| [doc/PRIVACY.md](./doc/PRIVACY.md) | [doc/PRIVACY_CN.md](./doc/PRIVACY_CN.md) | Privacy & data flow |
| [deploy/README.md](./deploy/README.md) | — | Docker, systemd, Windows service |

## Comparison

| | **dipper-bot** | **OpenClaw** | **Hermes Agent** |
|---|----------------|--------------|------------------|
| **Shape** | Single **Go** binary for core loop + gateway; optional small **Node** bridge only for WhatsApp. | **Node/TypeScript** agent platform with a rich **plugin** ecosystem (incl. LCM-style plugins such as lossless-claw). | **Multi-service** research agent (installer, gateway, broad channel & model integrations). |
| **Footprint & ops** | **Minimal runtime**: drop one binary, one workspace directory, edit JSON — easy to reason about on a small VPS or laptop. | More moving parts by design (plugins, UI conventions, platform features). | More processes and configuration surface. |
| **Long-context / recall** | **LCM built into the same binary** (`lcm_*` tools, SQLite DAG) — no separate plugin install for that path. | Powerful; LCM-style behavior often delivered as a **plugin** you install and wire up. | Strong **memory + nudges + skills** story tuned for continuous learning. |
| **Channels & tools** | **Many channels in-tree**; MCP via `tools.mcpServers`; file/shell/web/browser/cron/spawn. | Extensible via OpenClaw’s ecosystem and MCP. | Very broad tool and channel set; strong multi-channel story. |
| **Models** | OpenAI-compatible base URL + optional Codex/Copilot helpers. | Flexible via the wider stack. | OpenRouter / portals / custom endpoints.                     |

**When dipper-bot fits best** — you want a **small, auditable Go codebase**, **workspace-first** personal agent, **integrated LCM + MCP + cron** without assembling a large Node platform, and you are fine driving behavior through **JSON config** and a **thin CLI**.

## Acknowledgments

The following open-source projects informed dipper-bot's architecture and UX patterns (listed in no particular order):

| Project | Organization / upstream | Link |
|--------|-------------------------|------|
| **OpenClaw** | OpenClaw | [github.com/openclaw/openclaw](https://github.com/openclaw/openclaw) |
| **nanobot** | HKUDS | [github.com/HKUDS/nanobot](https://github.com/HKUDS/nanobot) |
| **Hermes Agent** | Nous Research | [github.com/NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) |

## License

MIT License. See [LICENSE](./LICENSE) in this project.
