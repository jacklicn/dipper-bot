# dipper-bot architecture

**Language:** English edition. Chinese: [DESIGN_CN.md](./DESIGN_CN.md).

## 1. Overview

**dipper-bot** is a small **Go** service shaped around: **gateway**, **message bus**, **agent loop**, **tools**, **sessions**, **cron**, **heartbeat**, and an **OpenAI-compatible** LLM provider layer.

### 1.1 Highlights

- **Single binary** for the core loop (optional **Node** bridge only for WhatsApp / Baileys).
- **Inbound bus** decouples channels from the agent; outbound bus delivers replies.
- **Tools:** files, shell, message, cron, spawn, web, memory, session search, LCM tools, MCP, channel helpers.
- **LCM** (optional): DAG + SQLite inspired by lossless-claw-style designs — summaries instead of blind truncation. See [LCM.md](./LCM.md).
- **Memory maintenance:** constrained background passes keep `USER.md` / `NOTE.md` + FTS session index aligned with tools.
- **Skills evolution:** autonomous create/patch of `skills/*/SKILL.md`, with optional mid-run reflection for long tool loops.
- **Learner feedback:** async memory/skill writes can notify users immediately (default) or via next-reply digest (`experience.learnerFeedbackInstantPush=false`).
- **Channels:** Telegram, WhatsApp, Discord, Feishu, DingTalk, Slack, Email, QQ, Wecom, Webhook (see [CONFIG.md](./CONFIG.md)).
- **Cron:** interval / cron expr / one-shot `at`, timezone-aware, per workspace.

### 1.2 Stack

- **Go** 1.26+ (see `go.mod`)
- **HTTP:** `net/http` gateway + provider calls
- **Config:** JSON (`~/.dipper-bot/config.json`, `<workspace>/config.json`)

### 1.3 CLI behavior

- **No args** (or double-click `.exe` on Windows): interactive REPL with subcommands.
- **`help` / `-h` / `--help`:** usage text.
- **Gateway default port:** 8090.

---

## 2. Message flow

1. Channel or `POST /message` enqueues an **inbound** event (channel, chat_id, sender, content).
2. **Session manager** resolves `channel:chat_id` (or CLI session id).
3. **Context builder** loads system prompt, memory fence, history, tool specs.
4. **Agent loop** calls the LLM; on tool calls, executes tools and loops until finish or `maxToolIterations`.
5. **Outbound** responses go back to the channel adapter or HTTP caller.

Same **session** receives **run cancellation** when a new user message arrives while a run is active.

---

## 3. Major packages (code map)

| Package | Responsibility |
|---------|------------------|
| `agent/` | Loop, context builder, memory, skills, adaptive controller, consolidator hooks |
| `tools/` | Tool registry + implementations (files, exec, web, memory/consolidator, LCM, MCP, …) |
| `lcm/` | SQLite schema, summarization jobs, `lcm_grep` / `lcm_describe` backing |
| `bus/` | Typed inbound/outbound queues |
| `gateway/` | HTTP surface (`/message`, health, etc.) |
| `channels/` | Telegram, WhatsApp bridge client, Discord, … |
| `session/` | JSONL persistence, ids |
| `cron/` | Job store + scheduler |
| `config/` | Schema + loader + JSON migrations |
| `providers/` | OpenAI-compatible chat completions |

---

## 4. Memory layers (conceptual)

1. **Working context** — recent turns + tool outputs in the prompt.
2. **Token memory** — when `contextWindowTokens` > 0, consolidator writes long-term summaries to `MEMORY.md` / `HISTORY.md`.
3. **Curated files** — `USER.md`, `NOTE.md` via `memory` tool + maintainer.
4. **LCM** — structured DAG for long histories (**on by default**; disable per workspace in `agents.defaults.lcm.enabled`).
5. **Session FTS** — `memory/sessions_fts.db` + `session_search` tool.
6. **User modeling (no Honcho integration)** — `USER.md` via the `memory` tool (`target: user`) is the curated user profile. The agent is instructed to reconcile contradictions (replace/remove stale lines) instead of stacking conflicting facts; together with session FTS this is dipper-bot’s intentional substitute for external “dialectic user modeling” stacks (e.g. Honcho in Hermes). **NOTE:** `save_memory` exists only inside `MemoryConsolidator` for `MEMORY.md` / `HISTORY.md`; the main agent tool is always `memory`.
7. **Learning feedback channel** — async learner events are routed to user-visible notices: immediate second outbound message by default; optional queued digest prepended to next assistant reply when instant push is disabled.

---

## 5. Security & sandboxing (high level)

- `tools.restrictToWorkspace` limits path access for file/exec tools.
- Exec timeouts from config.
- Untrusted channel input should be filtered at ingress (allowlists per channel where supported).

For **privacy / what is sent to the LLM**, see [PRIVACY.md](./PRIVACY.md).

---

## 6. Browser automation via MCP

- Browser automation is provided via `tools.mcpServers` (Chrome DevTools MCP), not built-in browser tools.
- Default generated config includes `tools.mcpServers.chrome-devtools` with `npx -y chrome-devtools-mcp@latest --no-performance-crux --no-usage-statistics` (no preset attach mode).
- For Chrome M144+, enable remote debugging at `chrome://inspect/#remote-debugging` before attaching.

---

## 7. Further reading

- [DESIGN_CN.md](./DESIGN_CN.md) — full Chinese design doc (longer diagrams & sections).
- [MANUAL.md](./MANUAL.md) — operations.
- [CONFIG.md](./CONFIG.md) — configuration (English summary + pointer to CONFIG_CN).
