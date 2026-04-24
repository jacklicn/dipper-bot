# Privacy & data flow

**Language:** English edition. Chinese: [PRIVACY_CN.md](./PRIVACY_CN.md).

This document summarizes **what data leaves the machine** when you use dipper-bot, especially **payloads sent to the LLM provider**.

---

## 1. End-to-end flow

1. **Input** arrives via CLI, Web UI, `POST /message`, or a configured **channel**.
2. **Inbound bus** carries channel id, sender id, chat id, text, timestamp.
3. **Agent loop** loads/creates a session, may run **MemoryConsolidator** (may call an LLM), builds messages (system + memory + history + current user text), runs the main tool loop.
4. Each **LLM** call sends the constructed chat to `provider.Chat()` (OpenAI-compatible or vendor-specific adapter).
5. **Tools** (`read_file`, `exec`, …) run **locally**; their outputs are appended to the chat and may trigger further LLM calls.
6. **Output** returns through the outbound path (channel reply, HTTP response, terminal).

---

## 2. What goes to the LLM provider

Typically includes:

- **System prompt** (tool descriptions, policy text, optional memory fence).
- **Conversation history** for the active session (subject to `memoryWindow`, LCM assembly, or consolidator-trimmed history).
- **Curated memory snippets** when injected (e.g. `USER.md` / `NOTE.md` excerpts).
- **Tool definitions** (JSON schema for callable tools).
- **Tool results** after each tool execution (file contents, command stdout, search snippets, etc.).

It does **not** silently upload unrelated files: only what the agent (or consolidator sub-prompt) reads and inserts into messages is transmitted.

---

## 3. Local-only components

- Filesystem reads/writes under your configured workspace rules.
- **Shell/exec** runs on the host running dipper-bot.
- **SQLite** for LCM and session index stays on disk unless you back it up elsewhere.
- **Channel bridges** (e.g. WhatsApp) talk to their respective vendor networks under **their** privacy policies.

---

## 4. Third-party services

- **LLM API** — subject to that vendor’s retention / logging policy.
- **Web search** — if configured (Brave, Tavily, Jina, SearXNG, etc.), queries go to the chosen engine.
- **MCP servers** — each stdio/HTTP MCP process is a separate subprocess; review each server’s behavior.

---

## 5. Practical minimization tips

- Use **allowlists** on channels where supported (`allowFrom`, etc.).
- Keep **secrets out of USER.md** unless you accept they may appear in future prompts.
- Set **`tools.restrictToWorkspace`** to reduce accidental path leakage into tools.
- Review **`providers.custom.apiBase`** so traffic goes only to intended endpoints.

For the **diagram-heavy Chinese version**, see [PRIVACY_CN.md](./PRIVACY_CN.md).
