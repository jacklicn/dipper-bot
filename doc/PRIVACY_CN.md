# dipper-bot 隐私与数据流说明

**语言**：本文为中文版；英文版见 [PRIVACY.md](./PRIVACY.md)。

本文档描述 dipper-bot 从用户输入到模型处理、本地执行、结果返回的完整数据流程，以及**哪些数据会上传到大模型**（LLM 提供商）。

---

## 一、整体数据流概览

```
用户输入 (CLI / Web / Gateway / Channels)
    │
    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Message Bus (Inbound)                                               │
│ Channel, SenderID, ChatID, Content, Timestamp                       │
└─────────────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Agent Loop (processMessage)                                         │
│ 1. 获取/创建会话                                                      │
│ 2. MemoryConsolidator（可能调用 LLM 归档）                             │
│ 3. BuildMessages（系统提示 + 历史 + 当前消息）                          │
│ 4. runAgentLoop（主循环）                                             │
└─────────────────────────────────────────────────────────────────────┘
    │
    ├──► LLM Provider.Chat()  ◄── 上传到 LLM 的数据（见第二节）
    │
    ├──► 本地工具执行 (read_file, write_file, exec 等)
    │         │
    │         └──► 工具执行结果追加到 messages，再次调用 LLM
    │
    └──► 结果返回 (Outbound / 直接响应)
```

---

## 二、上传到大模型（LLM）的数据

所有通过 `provider.Chat()` 发送给 LLM 提供商（OpenAI、Anthropic、OpenRouter、自定义 API 等）的请求包含以下数据：

### 2.1 系统提示（System Prompt）

| 数据项 | 来源 | 说明 |
|--------|------|------|
| 身份与能力描述 | 硬编码 | "You are dipper-bot, a helpful AI assistant..." |
| 当前时间 | 本地 | `time.Now().Format(...)` |
| 运行环境 | 本地 | `runtime.GOOS`, `runtime.GOARCH` |
| 工作区路径 | 配置 | `workspace` 的绝对路径 |
| MEMORY.md | 工作区 | `workspace/memory/MEMORY.md` 完整内容 |
| AGENTS.md | 工作区 | `workspace/AGENTS.md` |
| SOUL.md | 工作区 | `workspace/SOUL.md` |
| USER.md | 工作区 | `workspace/USER.md` |
| TOOLS.md | 工作区 | `workspace/TOOLS.md` |
| IDENTITY.md | 工作区 | `workspace/IDENTITY.md`（如存在） |
| Skills 摘要 | 工作区 skills/ | 技能名称与简要说明 |
| 当前会话 | 消息元数据 | `Channel: xxx`, `Chat ID: xxx` |

**隐私注意**：`USER.md`、`AGENTS.md`、`MEMORY.md` 通常包含用户相关信息，会完整发送给 LLM。

### 2.2 对话历史与当前消息

| 数据项 | 来源 | 说明 |
|--------|------|------|
| 历史消息 | Session JSONL | 最近 N 条或 LCM 组装后的摘要 + 尾部 |
| 当前用户消息 | 用户输入 | `msg.Content` |
| LCM 摘要 | LCM 存储 | 启用 LCM 时，压缩后的历史摘要 |

**隐私注意**：会话历史（用户与助手的对话内容）会发送给 LLM。

### 2.3 工具定义（Tools）

每次 Chat 请求都会附带所有已注册工具的定义：

- 工具名称
- 工具描述（可能包含工作区路径、`uploads/` 等提示）
- 参数 schema（JSON Schema）

工具定义本身不包含用户数据，但描述中可能暴露路径结构。

### 2.4 工具调用与结果（Tool Calls & Results）

当模型决定调用工具时，会发送：

- 助手消息（含 tool_calls：工具名 + 参数）
- 工具结果消息（tool 角色的 content）

**以下工具的结果会作为 tool 消息发送回 LLM：**

| 工具 | 上传的数据 |
|------|------------|
| read_file | 文件完整内容 |
| edit_file | 编辑后的片段（若模型请求） |
| list_dir | 目录列表 |
| exec | 命令及其**完整输出** |
| web_search | 搜索查询 + 返回的摘要片段 |
| web_fetch | URL + 抓取到的正文（markdown/text） |
| lcm_grep | 匹配到的消息/摘要内容 |
| lcm_describe | 对话概要 |
| message | 不产生发往 LLM 的结果（发送到通道） |
| cron | 任务添加/列表等结果 |
| spawn | 子任务完成摘要 |
| MCP_* | 取决于 MCP 服务器返回的内容 |

**隐私注意**：`read_file` 会上传文件内容；`exec` 会上传命令和输出；`web_fetch` 会上传页面内容。若用户在工作区内存放敏感文件，被读取后会上传。

---

## 三、其他会触发 LLM 调用的场景

### 3.1 LCM 压缩（启用 `lcm.enabled` 时）

- **触发**：对话历史超过阈值时，自动做 leaf/condensed 摘要
- **上传**：被压缩的对话片段（含 user/assistant 的 role 与 content）
- **目的**：生成摘要，替换长历史，减少 token

### 3.2 MemoryConsolidator（启用 `contextWindowTokens` 时）

- **触发**：对话 token 数超过上下文窗口时
- **上传**：当前 `MEMORY.md` + 待归档的会话消息（含 role、content、timestamp、tools used）
- **目的**：在 consolidator 专用 LLM 调用中通过内部 function **`save_memory`**（**非**主 Agent 可见工具名；主 Agent 精选记忆使用 **`memory`** → USER.md / NOTE.md）更新 MEMORY.md 和 HISTORY.md

---

## 四、上传到其他外部服务的数据

| 服务 | 数据 | 说明 |
|------|------|------|
| LLM 提供商 | 见第二节 | 所有 Chat 请求内容 |
| Groq（语音转写） | 音频文件 | `POST /api/transcribe` 时，若配置 Groq，音频会上传至 Groq Whisper。转写得到的文本会作为用户消息发送给 LLM |
| Vosk（本地转写） | 音频流 | 若配置 Vosk，音频发送到本地 WebSocket，不经过互联网。转写文本同样会作为用户消息发往 LLM |
| Web 搜索 | 搜索查询 | duckduckgo、brave、tavily、jina、searxng 等 |
| web_fetch | URL | 抓取目标 URL 的页面 |
| MCP 服务器 | 工具参数 | 取决于 MCP 配置（如 filesystem 会传路径） |

---

## 五、仅本地存储、不上传的数据

| 数据 | 存储位置 |
|------|----------|
| 会话历史（原始） | `workspace/sessions/*.jsonl` |
| LCM 数据库 | `workspace/lcm.db`（SQLite） |
| MEMORY.md | `workspace/memory/MEMORY.md` |
| HISTORY.md | `workspace/memory/HISTORY.md` |
| 上传文件 | `workspace/uploads/` |
| 配置文件 | `~/.dipper-bot/config.json`、`workspace/config.json` |

上述数据不会自动上传。但若模型通过工具（如 `read_file`）读取这些内容，**读取结果会作为 tool 消息发送给 LLM**。

---

## 六、数据流时序简图

```
1. 用户输入 content
2. processMessage()
   ├─ MemoryConsolidator.MaybeConsolidateByTokens()
   │   └─ 若触发 → provider.Chat(当前 MEMORY + 会话块) → 更新 MEMORY/HISTORY
   ├─ BuildMessages() → [system, history..., user: current]
   └─ runAgentLoop()
       │
       ├─ provider.Chat(messages, tools)  ← 首次上传
       │
       ├─ 若 model 返回 tool_calls:
       │   ├─ registry.Execute(toolName, args)  ← 本地执行
       │   ├─ AddToolResult(messages, result)  ← 结果加入 messages
       │   └─ provider.Chat(messages, tools)   ← 再次上传（含工具结果）
       │   └─ 循环直至无 tool_calls 或达最大迭代
       │
       ├─ sess.AddMessage() + Save()  ← 本地持久化
       └─ lcm.IngestTurn()  ← 若启用 LCM
           └─ 若触发 compaction → provider.Chat(对话块)  ← LCM 摘要上传
```

---

## 七、隐私保护建议

1. **敏感文件**：不要将密码、密钥、个人身份信息等放在工作区内，除非可接受被 LLM 读取。
2. **restrictToWorkspace**：启用 `tools.restrictToWorkspace: true`，限制文件操作在工作区内，避免误读系统文件。
3. **USER.md / MEMORY.md**：若内容敏感，可精简或脱敏后再写入。
4. **语音转写**：Groq 会接收音频；若需完全本地，使用 Vosk。
5. **MCP 服务器**：注意 MCP 工具可能访问并返回敏感数据，这些结果会传给 LLM。
6. **Web 搜索 / web_fetch**：查询和 URL 会发往对应服务，抓取结果会进 LLM 上下文。

---

*文档基于 dipper-bot 代码分析整理，如有遗漏以实际实现为准。*
