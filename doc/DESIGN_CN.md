dipper-bot 设计文档

**语言**：本文为中文版；英文版见 [DESIGN.md](./DESIGN.md)。

## 1. 项目概述

**dipper-bot** 是超轻量级个人 AI 助手，使用 Go 实现，核心概念：gateway、消息总线、agent 循环、工具、会话、cron 服务、heartbeat，以及 OpenAI 兼容的 LLM 提供商。

### 1.1 核心特性

- **Go 实现**：单二进制，无运行时依赖
- **消息总线架构**：通道与 Agent 解耦
- **LLM 提供商**：custom（OpenAI 兼容）、openai_codex（OAuth）、github_copilot（OAuth）
- **工具系统**：read_file、write_file、edit_file、list_dir、exec、message、cron、spawn、web_search、web_fetch、lcm_grep、lcm_describe、MCP
- **LCM 无损上下文**：参照 [lossless-claw](https://github.com/Martian-Engineering/lossless-claw)，DAG 摘要 + SQLite 持久化，替代滑动窗口截断
- **记忆维护驱动学习**：后台维护器以受约束流程维护 `USER.md` / `NOTE.md`，并结合会话索引与工具完成多层协作
- **技能自演化**：后台维护器可自主创建/修补 `skills/*/SKILL.md`，并支持执行期反思（mid-run reflection）
- **学习反馈通道**：异步学习事件默认即时第二条用户可见消息；可切换为下一条回复前置 digest
- **多通道**：Telegram、WhatsApp、Discord、Feishu、DingTalk、Slack、Email、QQ、Wecom、Webhook（插件式）
- **定时任务**：every / cron / at，支持时区

### 1.2 技术栈

- **语言**：Go 1.26+
- **HTTP**：net/http
- **配置**：JSON（~/.dipper-bot/config.json、workspace/config.json）

**CLI 行为**：
- **无参数**：进入交互式 REPL，显示帮助并等待输入命令（Windows 双击 exe 即进入）
- **help / -h / --help**：打印使用说明
- **Gateway 默认端口**：8090
- **LLM**：各提供商 HTTP API（OpenAI 兼容格式）

### 1.3 编译说明

**环境要求**：Go 1.26+（或 1.21+，需调整 go.mod）

#### 本机编译

| 平台 | 命令 | 输出 |
|------|------|------|
| **Linux** | `go build -o dipper-bot .` | `./dipper-bot` |
| **macOS** | `go build -o dipper-bot .` | `./dipper-bot` |
| **Windows** | `go build -o dipper-bot.exe .` | `.\dipper-bot.exe` |

#### 交叉编译

通过 `GOOS`、`GOARCH` 指定目标平台，无需目标机环境。建议使用 `CGO_ENABLED=0` 避免 CGO 依赖，便于跨平台；**Windows**：本机编译可免装 MinGW，从 Linux/macOS 交叉到 Windows 时几乎必须。`-ldflags="-s -w"` 可精简体积（可选）。

```bash
# Linux amd64（常见服务器）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dipper-bot .

# Linux arm64（树莓派、ARM 服务器）
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o dipper-bot .

# macOS amd64（Intel Mac）
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o dipper-bot .

# macOS arm64（Apple Silicon M1/M2/M3）
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o dipper-bot .

# Windows amd64
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dipper-bot.exe .

# Windows arm64
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags="-s -w" -o dipper-bot.exe .
```

**PowerShell（Windows）**：环境变量语法不同，用 `$env:VAR="value"` 并以分号分隔：

```powershell
# Linux amd64（常见服务器）
$env:CGO_ENABLED="0"; $env:GOOS="linux"; $env:GOARCH="amd64"; go build -ldflags="-s -w" -o dipper-bot .

# Linux arm64（树莓派、ARM 服务器）
$env:CGO_ENABLED="0"; $env:GOOS="linux"; $env:GOARCH="arm64"; go build -ldflags="-s -w" -o dipper-bot .

# macOS amd64（Intel Mac）
$env:CGO_ENABLED="0"; $env:GOOS="darwin"; $env:GOARCH="amd64"; go build -ldflags="-s -w" -o dipper-bot .

# macOS arm64（Apple Silicon M1/M2/M3）
$env:CGO_ENABLED="0"; $env:GOOS="darwin"; $env:GOARCH="arm64"; go build -ldflags="-s -w" -o dipper-bot .

# Windows amd64
$env:CGO_ENABLED="0"; $env:GOOS="windows"; $env:GOARCH="amd64"; go build -ldflags="-s -w" -o dipper-bot.exe .

# Windows arm64
$env:CGO_ENABLED="0"; $env:GOOS="windows"; $env:GOARCH="arm64"; go build -ldflags="-s -w" -o dipper-bot.exe .
```

---

## 2. 架构设计

### 2.1 整体架构

```
┌─────────────┐      Inbound       ┌──────────────┐      Outbound       ┌─────────────┐
│  Channels   │ ──────────────────>│ Message Bus  │ ───────────────────>│  Channels   │
│ (Telegram,  │  PublishInbound    │  (chan)      │  SubscribeOutbound  │ (Telegram,  │
│  WhatsApp,  │                    │              │  DispatchOutbound   │  WhatsApp,  │
│  Discord,…) │                    │              │                     │  Discord,…) │
└─────────────┘                    └──────┬───────┘                     └─────────────┘
       ▲                                  │                                    
       │                                  │ ConsumeInbound                     
       │                                  │ PublishOutbound                    
       │                                  ▼                                    
       │                             ┌──────────────┐                            
       │                             │  Agent Loop  │                            
       │                             │  - Context   │                            
       │                             │  - Registry  │                            
       │                             │  - Provider  │                            
       │                             │  - Session   │                            
       │                             └──────────────┘                            
       │                                    │
       └────────────────────────────────────┘
              Gateway (POST /message)
```

### 2.2 分层结构

```
    ┌───────────────────────────────────────────────────────────────────────────┐
    │                          CLI (main.go)                                    │
    │               REPL | help | onboard | agent | gateway                     │
    │               status | channels |  cron | provider login                  │
    │                                                                           │
    └───────────────────────────────────────────────────────────────────────────┘
                                      │
    ┌───────────────────────────────────────────────────────────────────────────┐
    │                        Gateway (gateway/)                                 │
    │                   POST /message → PublishInbound                          │
    └───────────────────────────────────────────────────────────────────────────┘
                                      │
    ┌───────────────────────────────────────────────────────────────────────────┐
    │                        Channels (channels/)                               │
    │       Telegram | WhatsApp | Discord | Feishu | DingTalk | … | Webhook     │
    │                Inbound: receive message → PublishInbound                  │
    │                Outbound: SubscribeOutbound → send message                 │
    └───────────────────────────────────────────────────────────────────────────┘
                                      │
    ┌───────────────────────────────────────────────────────────────────────────┐
    │                        Message Bus (bus/)                                 │
    │        inbound chan, outbound chan, subs map[channel][]cb                 │
    └───────────────────────────────────────────────────────────────────────────┘
                                      │
    ┌───────────────────────────────────────────────────────────────────────────┐
    │                        Agent Core (agent/)                                │
    │        AgentLoop | ContextBuilder | Registry | SessionManager | LCM       │
    └───────────────────────────────────────────────────────────────────────────┘
                                      │
    ┌───────────────────────────────────────────────────────────────────────────┐
    │                        Infrastructure                                     │
    │        config/ | cron/ | heartbeat/ | session/ | lcm/ | providers/        │
    └───────────────────────────────────────────────────────────────────────────┘
```

---

## 3. 核心模块

### 3.1 Message Bus (`bus/`)

**职责**：解耦通道与 Agent，提供异步消息传递。

**数据结构**：
- `InboundMessage`：Channel、SenderID、ChatID、Content、Timestamp
- `OutboundMessage`：Channel、ChatID、Content
- `MessageBus`：inbound chan、outbound chan、subs map

**关键方法**：
- `PublishInbound(ctx, msg)`：通道发布消息
- `ConsumeInboundWithTimeout(ctx, timeout)`：Agent 消费（带超时以便检查 ctx）
- `PublishOutbound(ctx, msg)`：Agent 发布回复
- `SubscribeOutbound(channel, cb)`：通道订阅回复
- `DispatchOutbound(ctx)`：goroutine 循环，将 outbound 分发给订阅者

**设计模式**：发布-订阅、生产者-消费者

### 3.2 Agent Loop (`agent/loop.go`)

**职责**：核心处理引擎，协调 LLM、工具、上下文、会话。

**处理流程**：
1. 消费 `InboundMessage`（或 ProcessDirect 直接处理）
2. 获取/创建会话（`SessionManager.GetOrCreate`）
3. 处理斜杠命令（/new、/help）
4. 设置工具上下文（message、cron、spawn、browser 的 channel/chatID；LCM 启用时设置 lcm_grep/lcm_describe 的 sessionKey）
5. LCM 启用时：`lcm.Bootstrap` 同步 session 历史到 LCM
6. 构建消息列表（`ContextBuilder.BuildMessages`：LCM 启用时从 LCM 组装上下文，否则用 session.GetHistory）
7. 调用 `runAgentLoop`：
   - 调用 `provider.Chat`
   - 若有 tool_calls，执行工具，追加结果，继续循环
   - 直到无 tool_calls 或达到 maxIter
8. 保存会话；LCM 启用时 `lcm.IngestTurn` 写入新消息并触发 compaction
9. 后台记忆维护器与技能维护器异步执行受约束流程：更新 `memory/USER.md` / `memory/NOTE.md`，并创建/修补 `skills/*/SKILL.md`
10. 学习反馈默认即时出站通知（`learnerFeedbackInstantPush=true`）；关闭后改为下一条回复前置 digest（互斥）
10. 发布 `OutboundMessage`

**运行取消**（gateway 模式）：同一会话收到新消息时，取消前一次正在执行的 run（`Run` 中 goroutine 处理，`sessionCancels` 映射存储 cancel）。`runTimeoutSec` 配置单次超时（秒），超时后 context 取消并返回 "Request timed out."。

### 3.3 Context Builder (`agent/context.go`)

**职责**：组装系统提示词和消息列表。

**组成**：
- `identity()`：时间、运行时、workspace 路径；说明 `memory` 工具与 USER 侧「单一致用户模型」维护（相对 Hermes/Honcho 类外部辩证用户建模的轻量替代）
- `loadBootstrap()`：AGENTS.md、SOUL.md、USER.md、TOOLS.md、IDENTITY.md
- `memory.GetMemoryContext()`：注入 `MEMORY.md`、`USER.md`、`NOTE.md` 记忆上下文
- `skills.BuildSummary()`：workspace/skills 下的 SKILL.md 列表

**LCM 集成**：`BuildMessages` 在 `lcm.enabled` 时从 `lcm.AssembleContext` 获取 context_items（summaries + fresh tail），按 token 预算组装；否则使用 `session.GetHistory`。

**Token-based memory**：当 `contextWindowTokens` > 0 时，`MemoryConsolidator` 在每次处理消息前检查预估 prompt token 数；若超过窗口，将旧消息块归档到 `memory/MEMORY.md` 和 `memory/HISTORY.md`（**独立** LLM 调用链内部使用名为 `save_memory` 的 function tool，**不**出现在主 Agent 工具列表中），并更新 `session.LastConsolidated`。`GetHistory(fromConsolidated=true)` 仅返回未归档消息。

**用户建模与 Hermes/Honcho**：dipper 不集成 Hermes 可选的 [Honcho 记忆 / 辩证用户建模](https://hermes-agent.nousresearch.com/docs/user-guide/features/honcho) 提供方。跨会话的用户画像与偏好演进由 **`memory` 工具写 USER.md**（及后台 `MemoryMaintainer`）、**`session_search`（FTS + 摘要）** 与 **`NOTE.md`** 共同承担；系统提示中要求对矛盾陈述做 **replace/remove** 以保持 USER.md 内一致，从而在单工作区内实现与「深度用户建模」相近的**可运维**目标，而非 1:1 复刻 Honcho 产品形态。

**职责区分**：
- **MemoryConsolidator**：从对话中抽取事实和事件 → MEMORY.md + HISTORY.md
- **LCM**：压缩对话历史 → DAG 摘要 → 组装对话上下文
- **MemoryMaintainer**：后台 review/fork 风格记忆维护，仅通过 memory tool 更新记忆文件

### 3.4 Tool Registry (`tools/`)

**Tool 接口**：
```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]any  // OpenAI function schema
    Execute(ctx, params) (string, error)
}
```

**内置工具**：
| 工具 | 说明 |
|------|------|
| read_file | 读取文件，路径相对 workspace |
| write_file | 写入文件 |
| edit_file | 查找替换编辑 |
| list_dir | 列出目录 |
| exec | 执行 shell 命令，支持 Python venv |
| message | 发送消息到通道 |
| cron | 添加/列出/删除定时任务 |
| spawn | 后台子 Agent |
| web_search | 多引擎（Brave、Tavily、Jina、SearXNG、DuckDuckGo），缺凭证时回退 DuckDuckGo |
| web_fetch | 抓取 URL 内容 |
| sessions_list | 列出所有会话（OpenClaw 风格） |
| sessions_history | 获取指定会话的消息历史 |
| sessions_send | 向另一会话发送消息（触发 Agent 处理） |
| lcm_grep | 按正则搜索 LCM 压缩历史（仅 LCM 启用时） |
| lcm_describe | 获取压缩历史的高层描述（仅 LCM 启用时） |
| MCP_* | MCP 服务器注册的工具 |

**浏览器自动化**：通过 `tools.mcpServers` 接入 Chrome DevTools MCP（如 `chrome-devtools-mcp`），用于 live 会话调试和复用登录态。

**路径解析**：`resolvePath(path, workspaceDir, allowedDir)` — 相对路径基于 workspace，可选 allowedDir 限制。

### 3.5 LCM 无损上下文 (`lcm/`)

**职责**：压缩对话历史 → DAG 摘要 → 组装对话上下文。参照 OpenClaw lossless-claw，实现 DAG 摘要式无损上下文管理，替代滑动窗口截断。

**数据模型**（SQLite）：
- `conversations`：session_id → conversation_id
- `messages`：原始消息（seq、role、content、token_count）
- `summaries`：leaf 或 condensed 摘要节点
- `summary_messages`：leaf 摘要 → 源消息
- `summary_parents`：condensed 摘要 → 父摘要
- `context_items`：有序列表（message 或 summary 引用）

**流程**：
1. **Bootstrap**：将 session 的 JSONL 历史同步到 LCM
2. **Assemble**：从 context_items 取 evictable 前缀 + protected fresh tail，按 token 预算组装
3. **IngestTurn**：每轮对话后写入新消息，超阈值时触发 compaction
4. **Compaction**：将最老的连续 raw messages 用 LLM 摘要为 leaf summary，替换 context_items 中的区间

**配置**（`agents.defaults.lcm`）：
- `enabled`：是否启用
- `contextThreshold`：触发 compaction 的 token 比例（默认 0.75）
- `freshTailCount`：保护不被压缩的最近消息数（默认 32）
- `leafMinFanout`：最少消息数才做 leaf compaction（默认 8）
- `leafChunkTokens`：单次 leaf 源 token 上限（默认 20000）
- `leafTargetTokens`：leaf 摘要目标 token（默认 1200）
- `incrementalMaxDepth`：0=仅 leaf，-1=无限 condensation

**存储**：`workspace/lcm.db`（或 `databasePath` 指定）

### 3.6 Session Manager (`session/`)

**职责**：持久化会话状态（JSONL）。

**存储**：`workspace/sessions/{SafeFilename(key)}.jsonl`

**SessionKey**：`channel:chatID`（如 `telegram:123456`）

### 3.7 Cron Service (`cron/`)

**职责**：定时任务调度。

**Schedule 类型**：
- `every`：每 N 毫秒
- `cron`：cron 表达式，可选时区
- `at`：一次性，指定时间戳

**存储**：`workspace/cron/jobs.json`（按工作区隔离，支持多实例）

**执行**：`OnJob` 回调调用 `AgentLoop.ProcessDirect`，将 job 的 message 交给 Agent。

### 3.8 Heartbeat Service (`heartbeat/`)

**职责**：定期检查 `workspace/HEARTBEAT.md`，若有任务则调用 Agent 执行。

**间隔**：默认 30 分钟。

### 3.9 Providers (`providers/`)

**LLMProvider 接口**：
```go
type LLMProvider interface {
    Chat(ctx, req) (*LLMResponse, error)
    GetDefaultModel() string
}
```

**实现**：custom（OpenAI 兼容）、openai_codex（OAuth）、github_copilot（OAuth）。配置 `providers.custom.apiKey` 和可选的 `providers.custom.apiBase`。

### 3.10 Config (`config/`)

**结构**：Agents、Providers、Gateway、Tools、Channels

**路径**：`~/.dipper-bot/config.json`

**Provider 配置**：仅 custom，含 apiKey、apiBase、extraHeaders

**Tools 配置**：
- `tools.web.search`：多引擎（provider、apiKey、baseUrl、maxResults）；provider 默认 duckduckgo，可选 brave、tavily、jina、searxng
- `tools.web.proxy`：web_search、web_fetch 代理
- `tools.mcpServers`：每项支持 `headers`（自定义认证）、`toolTimeout`（秒）、`enabledTools`（工具白名单）
- `tools.mcpServers.chrome-devtools`：Chrome DevTools MCP（默认不预设附加模式；可按需使用 `--browser-url` / `--auto-connect` 等）
- `agents.defaults.runTimeoutSec`：单次对话最大执行秒数，0 不限制；同会话新消息会中止前一次

**配置分离**：
- **工作路径**：保存在 `~/.dipper-bot/config.json` 的 `agents.defaults.workspace`
- **其他配置**（providers、channels、gateway 等）：保存在工作区内的 `workspace/config.json`

**配置优先级**：工作区配置（`workspace/config.json`）优先于默认路径（`~/.dipper-bot/config.json`）。

**加载逻辑**（agent、gateway、status、channels、cron 等命令）：
- **无 --workspace**：从 `~/.dipper-bot/config.json` 读取工作路径；若存在 `workspace/config.json` 则加载该配置
- **有 --workspace**：优先使用 `workspace/config.json`，否则回退到 `~/.dipper-bot/config.json`
 
---

## 4. 数据流

### 4.1 Gateway 模式（dipper-bot gateway）

```
用户 → Telegram/WhatsApp/Discord
        → Channel 接收
        → PublishInbound(bus)
        → AgentLoop.Run() 消费
        → 同会话新消息：cancelSessionRun 取消前一次
        → goroutine 中 processMessage → LLM + Tools
        → PublishOutbound(bus)
        → DispatchOutbound 分发
        → Channel.Send() 发送回复
```

### 4.2 CLI 模式（dipper-bot agent）

```
用户输入 → ProcessDirect(content, sessionKey, ...)
         → processMessage（同 gateway）
         → 返回 response 文本
         → 打印到 stdout
```

### 4.3 Web 模式（dipper-bot agent --web）

```
浏览器 → GET / 获取聊天页面（HTML）
      → POST /api/chat { content } 发送消息
      → ProcessDirect(content, "web:default", "web", "default", nil)
      → 返回 JSON { content } 或 { error }
      → POST /api/upload（multipart file）上传文件
      → 保存到 workspace/uploads/，返回 { paths: ["uploads/xxx"] }
```

**接口**：
- **GET /** — 聊天页面
- **POST /api/chat** — 请求体 `{"content": "..."}`，返回 `{"content": "..."}` 或 `{"error": "..."}`，超时 5 分钟
- **POST /api/upload** — multipart/form-data 字段 `file`，单文件最大 50MB，保存到 `workspace/uploads/`，返回 `{"paths": ["uploads/文件名"]}`

**启动**：`dipper-bot agent --web [--host HOST] [-p PORT]`，默认端口 8600，会话 `web:default`。

### 4.4 Cron 模式

```
CronService.loop() 定时触发
  → OnJob(job)
  → ProcessDirect(job.Payload.Message, "cron:"+job.ID, ...)
  → 可选：Deliver 时 PublishOutbound 到通道
```

---

## 5. 目录结构

```
dipper-bot/
├── main.go              # CLI 入口
├── lcm/                 # 无损上下文管理
│   ├── config.go        # LCM 配置
│   ├── types.go         # ContextItem、Summary、MessageRow
│   ├── store.go         # SQLite 存储
│   ├── assembler.go     # 上下文组装
│   ├── compaction.go    # Leaf compaction
│   └── engine.go        # 主入口
├── agent/               # Agent 核心
│   ├── loop.go          # AgentLoop
│   ├── context.go       # ContextBuilder
│   ├── memory.go        # MemoryStore
│   ├── memory_maintainer.go  # 后台记忆维护器（受约束流程）
│   ├── skills.go        # SkillsLoader
│   └── subagent.go      # SubagentManager
├── tools/               # 工具实现
│   ├── base.go          # Tool 接口
│   ├── registry.go      # Registry
│   ├── filesystem.go    # read_file、write_file、edit_file、list_dir
│   ├── exec.go          # exec 执行 shell 命令
│   ├── message.go       # message 发送消息到通道
│   ├── cron.go          # cron 定时任务
│   ├── spawn.go         # spawn 后台子 Agent
│   ├── web.go           # web_search（多引擎）、web_fetch
│   ├── lcm.go           # lcm_grep、lcm_describe
│   ├── consolidator.go  # MemoryConsolidator（token-based 归档）
│   ├── sessions.go      # sessions_list、sessions_history、sessions_send（OpenClaw 风格）
│   └── mcp.go           # MCP 服务器连接与工具注册
├── bus/                 # 消息总线
├── config/              # 配置
├── cron/                # 定时任务
├── gateway/             # HTTP 网关
├── web/                 # 网页版对话（agent --web）
│   ├── server.go        # HTTP 服务：GET /、POST /api/chat、POST /api/upload、POST /api/transcribe
│   └── static/          # 嵌入的聊天页面（chat.html）
├── heartbeat/           # 心跳任务
├── providers/           # LLM 提供商、语音转写（Groq Whisper / Vosk 本地）
├── session/             # 会话管理（JSONL）
├── channels/            # 通道（Telegram/WhatsApp/Discord/Feishu/DingTalk/Slack/Email/QQ/Wecom/Webhook）
├── os/                  # 平台相关
│   ├── win32/           # Windows 控制台 UTF-8（SetConsoleOutputCP、VT 模式）
│   └── unix/            # 非 Windows 占位
├── utils/               # 工具函数
└── deploy/              # 部署示例（Docker、systemd、Windows 服务）
```

---

## 6. 设计决策

### 6.1 同步 vs 异步

- Go 使用 goroutine 实现并发，无 asyncio
- MessageBus 使用 channel 实现异步队列
- AgentLoop.Run、DispatchOutbound、Channels 各自在 goroutine 中运行

### 6.2 工具执行

- 工具为同步执行，`Execute(ctx, params) (string, error)`
- exec 工具使用 `exec.CommandContext` + 超时
- Python 脚本在 workspace/.venv 中执行

### 6.3 会话持久化

- **JSONL**：`workspace/sessions/{key}.jsonl`，每行一条消息或 metadata，按 sessionKey 分文件
- **LCM**（可选）：`workspace/lcm.db`，SQLite 持久化全部消息与摘要 DAG，用于无损上下文组装

### 6.4 安全

- exec：deny 模式黑名单、working_dir 校验（RestrictToWorkspace）
- 文件工具：WorkspaceDir + AllowedDir 路径限制
- web_fetch：SSRF 防护，禁止私有/loopback IP

### 6.5 Chrome DevTools MCP attach 模式

- **chrome-devtools-mcp**（MCP 服务器）：默认不预设附加模式；可用 `--browser-url` 连固定 CDP，或用 `--auto-connect` 附加已登录 live Chrome（M144+ 需 `chrome://inspect/#remote-debugging`）
- 默认生成的 `workspace/config.json` 已包含 `tools.mcpServers.chrome-devtools`，默认 `args` 为 `npx -y chrome-devtools-mcp@latest --no-performance-crux --no-usage-statistics`
- 复用 cookies 和登录状态，适合需认证的页面调试；Chrome M144+ 需在 `chrome://inspect/#remote-debugging` 启用远程调试
- 通过 `tools.mcpServers` 配置接入，替代内置 browser 工具

### 6.7 通道插件（Webhook）

- **Webhook 通道**：HTTP 服务监听 POST，将外部消息注入 bus，实现插件式通道扩展
- 外部服务或脚本通过 `POST /message` 发送 `{"sender","chat_id","text"}` 即可触发 Agent
- Channel Plugin，支持自定义通道或自动化集成
