# dipper-bot 配置参数详解

**语言**：本文为中文版；英文版见 [CONFIG.md](./CONFIG.md)。

本文档根据项目最新功能，完整说明 `config.json` 中所有参数的含义、取值、默认值及示例。

**CLI 提示**：无参数启动（或 Windows 双击 exe）进入交互式 REPL；`help` / `-h` / `--help` 打印使用说明；Gateway 默认端口 8090。

**多实例**：Cron 存储在 `workspace/cron/jobs.json`，WhatsApp 认证存储在 `workspace/whatsapp-auth`，每个工作区独立。多实例时需为不同 workspace 配置不同端口和通道（如 Telegram 不同 bot、WhatsApp 不同 bridgeUrl）。

## 配置文件位置

- **默认配置**：`~/.dipper-bot/config.json`（仅存储 `workspace` 路径）
- **完整配置**：`<workspace>/config.json`（工作区内的主配置文件）
- **优先级**：工作区配置覆盖默认配置

## config.json 完整结构索引

```
config.json
├── agents
│   └── defaults
│       ├── workspace          string
│       ├── model              string
│       ├── maxTokens          int
│       ├── temperature        float
│       ├── maxToolIterations  int
│       ├── memoryWindow       int
│       ├── contextWindowTokens int (可选，0=不启用 token-based memory)
│       ├── reasoningEffort    string (可选)
│       ├── runTimeoutSec      int (可选)
│       └── lcm
│           ├── enabled               bool
│           ├── databasePath          string (可选)
│           ├── contextThreshold      float
│           ├── freshTailCount        int
│           ├── leafMinFanout         int
│           ├── condensedMinFanout    int
│           ├── leafChunkTokens       int
│           ├── leafTargetTokens      int
│           ├── condensedTargetTokens  int
│           └── incrementalMaxDepth   int
├── providers
│   ├── custom
│   │   ├── apiKey       string
│   │   ├── apiBase      string
│   │   └── extraHeaders object (可选)
│   ├── groq
│   │   └── apiKey       string
│   └── transcription
│       ├── provider     string (groq|vosk)
│       └── voskUrl      string (可选)
├── gateway
│   ├── host    string
│   └── port    int
├── logging (可选，进程级 slog；默认启用)
│   ├── enabled      bool (可选，默认 true)
│   ├── level        string (debug|info|warn|error，默认 info)
│   ├── maxAgeDays   int (删除超过 N 天的旧日志，默认 7)
│   ├── maxSizeMB    int (单文件超过 N MiB 滚动，默认 128)
│   ├── maxBackups   int (可选，最多保留几个备份文件；0=仅按天数清理)
│   ├── fileOnly     bool (仅写文件、不复制到 stderr)
│   ├── dir          string (工作区内子目录，默认 logs)
│   ├── filename     string (日志文件名，默认 dipper-bot.log)
│   └── compress     bool (是否 gzip 历史文件)
├── tools
│   ├── web
│   │   ├── search
│   │   │   ├── provider   string (duckduckgo|brave|tavily|jina|searxng，默认duckduckgo)
│   │   │   ├── apiKey     string
│   │   │   ├── baseUrl    string (SearXNG 专用)
│   │   │   └── maxResults int
│   │   └── proxy         string (可选)
│   ├── exec
│   │   └── timeout      int
│   ├── restrictToWorkspace  bool
│   └── mcpServers       object (可选，每项支持 headers/toolTimeout/enabledTools)
└── channels
    ├── whatsapp    { enabled, bridgeUrl, bridgeToken, allowFrom }
    ├── telegram    { enabled, token, proxy, allowFrom }
    ├── discord     { enabled, token, gatewayUrl, intents, allowFrom }
    ├── feishu      { enabled, appId, appSecret, encryptKey, verificationToken, allowFrom }
    ├── mochat      { enabled, baseUrl, socketUrl, ... }
    ├── dingtalk    { enabled, clientId, clientSecret, allowFrom }
    ├── email       { enabled, consentGranted, imap*, smtp*, ... }
    ├── slack       { enabled, mode, botToken, appToken, dm, ... }
    ├── qq          { enabled, appId, secret, allowFrom }
    ├── wecom       { enabled, botId, secret, allowFrom, welcomeMessage }
    └── webhook     { enabled, port, path, allowFrom }
```

---

## 一、Agent 配置 `agents.defaults`

### 1.1 基础参数

#### `workspace`

**含义**：工作区根目录路径，用于存放会话、上传文件、技能、记忆等。

**类型**：`string`  
**默认值**：`~/.dipper-bot/workspace`  
**支持**：`~` 会展开为用户主目录；`~/.dipper-bot/workspace` 或 `/home/user/my-workspace` 均可。

**示例**：

```json
"workspace": "~/projects/dipper-workspace"
```

---

#### `model`

**含义**：调用的 LLM 模型标识。根据前缀选择不同的 provider。

**类型**：`string`  
**默认值**：`gpt-4`

**示例**：

| 格式 | 含义 | 示例 |
|------|------|------|
| 直接模型名 | 使用 `custom` provider（OpenAI 兼容） | `"gpt-4o"`、`"claude-3-5-sonnet"` |
| `provider/model` | 通过 OpenRouter 等网关 | `"anthropic/claude-3-5-haiku"` |
| `openai-codex/` | OpenAI Codex OAuth | `"openai-codex/gpt-5.1-codex"` |
| `github-copilot/` | GitHub Copilot OAuth | `"github-copilot/gpt-4o"` |

---

#### `maxTokens`

**含义**：单次回复允许的最大 token 数（输出上限）。超出会截断。

**类型**：`int`  
**默认值**：`8192`  
**推荐范围**：简单任务 2048–4096，复杂任务 4096–8192。

**示例**（省 token 时用）：

```json
"maxTokens": 4096
```

---

#### `temperature`

**含义**：采样温度。越高输出越随机、越有创意；越低越确定、越重复。

**类型**：`float`  
**默认值**：`0.7`  
**范围**：通常 0–2；部分模型（如 kimi-k2.5）要求 ≥ 1.0。

**示例**：

```json
"temperature": 0.3
```

（代码、事实类任务）

```json
"temperature": 1.0
```

（创意写作）

---

#### `maxToolIterations`

**含义**：单轮对话中，Agent 最多可执行多少轮「LLM 决策 → 工具调用 → 结果」循环。

**类型**：`int`  
**默认值**：`20`  
**说明**：每轮可能包含多个工具调用；达到上限后不再继续。配置为 `0` 或负数时，会回退到默认值 `20`。

**示例**：

```json
"maxToolIterations": 15
```

（省 token）

```json
"maxToolIterations": 30
```

（复杂多步骤任务）

---

#### `memoryWindow`

**含义**：未启用 LCM 且未启用 token-based memory 时，保留的最近对话条数（user+assistant 各算一条）。超过部分会被截断。

**类型**：`int`  
**默认值**：`50`  
**说明**：启用 LCM 后，此参数不再用于历史截断，由 LCM 管理上下文。启用 `contextWindowTokens` 后，由 MemoryConsolidator 按 token 归档。

**示例**：

```json
"memoryWindow": 24
```

（短对话、省 token）

```json
"memoryWindow": 80
```

（长对话、无 LCM）

---

#### `contextWindowTokens`

**含义**：Token-based memory（MemoryConsolidator）。MemoryConsolidator：从对话中抽取事实和事件 → MEMORY.md + HISTORY.md。当 >0 时，若预估 prompt 超过此值，将旧消息归档到 `memory/MEMORY.md` 和 `memory/HISTORY.md`，直到 prompt 低于一半窗口。

**类型**：`int`  
**默认值**：`0`（不启用，使用 memoryWindow 滑动窗口）  
**示例**（128K 上下文）：

```json
"contextWindowTokens": 131072
```

---

#### `reasoningEffort`

**含义**：推理强度，用于 o1、o3、Codex 等 thinking 模型。

**类型**：`string`  
**可选值**：`"low"`、`"medium"`、`"high"`  
**默认**：未设置（由模型默认）

**示例**（平衡速度与质量）：

```json
"reasoningEffort": "medium"
```

---

#### `runTimeoutSec`

**含义**：单次 Agent 运行的最大秒数。超时后自动终止；新消息会取消同会话的旧运行。

**类型**：`int`  
**默认值**：`0`（无限制）

**示例**（5 分钟超时）：

```json
"runTimeoutSec": 300
```

---

### 1.2 LCM（Lossless Context Management）

LCM：压缩对话历史 → DAG 摘要 → 组装对话上下文。将历史对话压缩为摘要，在保持上下文的同时显著减少 token 消耗。

#### `lcm.enabled`

**含义**：是否启用 LCM。启用后，历史由 LCM 管理，而非简单滑动窗口。

**类型**：`bool`（配置中省略 `lcm` 或省略 `enabled` 时，`LoadConfig` 合并为 **开启**；显式 `false` 关闭）

**默认值**：`true`

**示例**（关闭 LCM）：

```json
"enabled": false
```

---

#### `lcm.databasePath`

**含义**：LCM 使用的 SQLite 数据库路径。空则使用工作区内默认路径。

**类型**：`string`  
**默认值**：空

**示例**：

```json
"databasePath": "/var/lib/dipper-bot/lcm.db"
```

---

#### `lcm.contextThreshold`

**含义**：上下文 token 预算占 `maxTokens` 的比例（0–1）。用于控制组装给 LLM 的上下文总长度。

**类型**：`float`  
**默认值**：`0.75`

**示例**：

```json
"contextThreshold": 0.7
```

（更保守）

```json
"contextThreshold": 0.9
```

（更宽松）

---

#### `lcm.freshTailCount`

**含义**：保留的最近完整消息条数（不被压缩）。这些消息始终以原文形式发送给 LLM。

**类型**：`int`  
**默认值**：`32`

**示例**：

```json
"freshTailCount": 20
```

（省 token）

```json
"freshTailCount": 40
```

（保留更多近期上下文）

---

#### `lcm.leafMinFanout` / `lcm.condensedMinFanout`

**含义**：DAG 摘要结构中的叶子/压缩节点最小扇出数。用于控制摘要树的形状。

**类型**：`int`  
**默认值**：`8` / `4`  
**说明**：一般无需调整。

---

#### `lcm.leafChunkTokens`

**含义**：单块叶子消息的 token 数上限，用于分块。

**类型**：`int`  
**默认值**：`20000`

---

#### `lcm.leafTargetTokens`

**含义**：叶子摘要的目标 token 数。越小摘要越短，省 token 越明显。

**类型**：`int`  
**默认值**：`1200`

**示例**（省 token）：

```json
"leafTargetTokens": 1000
```

---

#### `lcm.condensedTargetTokens`

**含义**：压缩摘要的目标 token 数。

**类型**：`int`  
**默认值**：`2000`

**示例**（省 token）：

```json
"condensedTargetTokens": 1500
```

---

#### `lcm.incrementalMaxDepth`

**含义**：增量压缩的最大深度。`-1` 表示不限制。

**类型**：`int`  
**默认值**：`-1`

---

**LCM 完整示例**：

```json
"lcm": {
  "enabled": true,
  "contextThreshold": 0.75,
  "freshTailCount": 24,
  "leafMinFanout": 8,
  "condensedMinFanout": 4,
  "leafChunkTokens": 20000,
  "leafTargetTokens": 1000,
  "condensedTargetTokens": 1500,
  "incrementalMaxDepth": -1
}
```

---

### 1.3 学习与自演化配置（`memoryMaintenance` / `skillsEvolution`）

当前版本中，学习能力由两条并行后台流程驱动，均通过 `agents.defaults` 下的配置项控制：

- `memoryMaintenance`：维护 `memory/USER.md` 与 `memory/NOTE.md`
- `skillsEvolution`：根据高频工具工作流自动创建/修补 `skills/*/SKILL.md`

两者都支持：

- 开关：`enabled`（JSON 省略该字段时与 `LoadConfig` 合并后视为 **开启**，与 `false` 显式关闭区分）
- 质量门槛：`minQualityScore`、`minConfidence`
- 节流与抑制：`minIntervalMinutes`、`repeatSuppressionMinutes`、`failureBackoffMinutes`
- 自适应控制器（PID 风格）：`controllerTargetBadRate`、`controllerKp/Ki/Kd`、`controllerBatchSize`、`controllerMinFloor/MaxFloor`、`controllerOnlineTuning`

#### 1.3.1 `memoryMaintenance` 关键字段

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `true` | 是否启用记忆维护；整块省略时默认开启 |
| `queueSize` | int | `64` | 后台队列长度 |
| `nudgeInterval` | int | `1` | 每 N 轮会话触发一次候选评估 |
| `flushMinTurns` | int | `6` | 会话 flush 时至少多少用户轮次才执行 |
| `semanticRegroupMaxEntries` | int | `40` | 语义重分组最大条目数 |
| `semanticRegroupMaxGroups` | int | `6` | 语义重分组最大主题组数 |

#### 1.3.2 `skillsEvolution` 关键字段

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `true` | 是否启用技能自演化；整块省略时默认开启 |
| `creationNudgeInterval` | int | `15` | 每个 session 每 N 轮考虑一次技能演化 |
| `minToolCalls` | int | `5` | 当前轮工具调用数至少达到该值才进入候选 |
| `flushMinToolCalls` | int | `5` | lifecycle flush 时最少工具调用门槛 |
| `midRunReflectEveryToolIters` | int | `4` | 同一用户轮内，每完成 N 次「模型→工具」循环后尝试一次技能反思（额外 LLM）；`0` 关闭 |
| `midRunReflectMinSeconds` | int | `120` | 同一 session 两次执行期反思的最小间隔（秒）；`0` 表示与默认 120 相同 |

**触发要点**：即使 `enabled=true`，仍需满足轮次、工具调用量、质量/置信度门槛，且可能被重复抑制与失败退避拦截，因此不是“每轮都写技能”。**执行期反思**在长跑工具轮中提前沉淀/修补 `SKILL.md`，与回合结束入队、lifecycle flush 互补。**用户反馈**：执行期技能成功写入时附在**当前**主回复末尾。异步 worker / flush 写入的技能与记忆维护成功时，默认走 `agents.defaults.experience.learnerFeedbackInstantPush=true` 的**即时第二条气泡**；若设为 `false`，则改为进入**待发送队列**并在下一条助手回复开头插入「学习反馈 / Learner digest」块（两种模式互斥，不重复）。

---

## 二、Providers 配置 `providers`

### 2.1 Custom（OpenAI 兼容）

#### `providers.custom.apiKey`

**含义**：API 密钥，用于调用 OpenAI 兼容接口。

**类型**：`string`  
**示例**：

```json
"apiKey": "xxxxxxxxxxxxxxxx"
```

---

#### `providers.custom.apiBase`

**含义**：API 基础 URL。空则使用默认 `https://api.openai.com/v1`。

**类型**：`string`  
**示例**：

```json
"apiBase": "https://api.openai.com/v1"
```

（OpenAI）

```json
"apiBase": "https://openrouter.ai/api/v1"
```

（OpenRouter）

---

#### `providers.custom.extraHeaders`

**含义**：额外的 HTTP 请求头。

**类型**：`object`  
**示例**：

```json
"extraHeaders": {
  "X-Custom-Header": "value",
  "X-API-Version": "2026-01-01",
  "X-Request-ID": "optional-trace-id",
  "X-Client-Version": "1.0.0"
}
```

---

**Custom 完整示例**：

```json
"providers": {
  "custom": {
    "apiKey": "sk-xxxxxxxx",
    "apiBase": "https://api.openai.com/v1"
  }
}
```

---

### 2.2 Groq（语音转写）

#### `providers.groq.apiKey`

**含义**：Groq API 密钥，用于 Whisper 语音转写（Web 端麦克风输入）。

**类型**：`string`  
**示例**：

```json
"apiKey": "gsk_xxxxxxxx"
```

---

### 2.3 Transcription（语音输入）

#### `providers.transcription.provider`

**含义**：语音转写后端。`groq` 为云端，`vosk` 为本地。

**类型**：`string`  
**可选值**：`"groq"`、`"vosk"`

---

#### `providers.transcription.voskUrl`

**含义**：Vosk WebSocket 服务地址（仅当 `provider` 为 `vosk` 时有效）。

**类型**：`string`  
**默认值**：`ws://localhost:2700`  
**示例**：

```json
"voskUrl": "ws://192.168.1.100:2700"
```

---

**Transcription 完整示例**：

```json
"providers": {
  "transcription": {
    "provider": "groq"
  },
  "groq": {
    "apiKey": "gsk_xxxxxxxx"
  }
}
```

---

## 三、Gateway 配置 `gateway`

#### `gateway.host`

**含义**：Gateway HTTP 服务监听地址。空或 `0.0.0.0` 表示监听所有接口。

**类型**：`string`  
**示例**：

```json
"host": ""
```

（所有接口）

```json
"host": "127.0.0.1"
```

（仅本机）

---

#### `gateway.port`

**含义**：Gateway 监听端口。

**类型**：`int`  
**默认值**：`8090`  
**示例**：

```json
"port": 8090
```

---

#### `gateway.rateLimitPerMinute`

**含义**：网关每分钟请求限流阈值。

**类型**：`int`  
**默认值**：`120`

---

#### `gateway.rateLimitIPv4Prefix` / `gateway.rateLimitIPv6Prefix`

**含义**：按 IP 聚合时的前缀长度（用于限流桶）。

**类型**：`int`  
**默认值**：`32` / `128`

---

#### `gateway.rateLimitCidrs`

**含义**：可选 CIDR 列表，用于自定义限流聚合范围。

**类型**：`[]string`  
**默认值**：空

---

**Gateway 示例**：

```json
"gateway": {
  "host": "0.0.0.0",
  "port": 8090
}
```

---

## 三（附）、Logging 配置 `logging`

进程级 `slog` 输出：`dipper-bot agent`、`gateway`、`cron` 在加载配置后写入 **工作区** 下的滚动日志（默认 `<workspace>/logs/dipper-bot.log`），并可同时镜像到 stderr（可用 `fileOnly` 关闭镜像）。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | `bool` | **true**（省略时与默认配置合并为开启） | `false` 时仅 stderr，不写文件。 |
| `level` | `string` | `info` | `debug` / `info` / `warn` / `error`。`gateway --verbose` 会强制为 `debug`。 |
| `maxAgeDays` | `int` | **7** | 超过 N 天的历史滚动文件会被删除（lumberjack `MaxAge`）。 |
| `maxSizeMB` | `int` | **128** | 当前日志文件超过 N MiB 时滚动到新文件。 |
| `maxBackups` | `int` | `0` | 可选：最多保留多少个备份文件；`0` 表示不按个数上限，仅配合 `maxAgeDays` 清理。 |
| `fileOnly` | `bool` | `false` | `true` 时只写文件，不复制到 stderr（适合 systemd 已接管日志的场景）。 |
| `dir` | `string` | `logs` | 工作区下的子目录名。 |
| `filename` | `string` | `dipper-bot.log` | 日志文件名（仅取 basename，防止路径穿越）。 |
| `compress` | `bool` | `false` | 是否 gzip 压缩已滚动的历史文件。 |

**示例**（关闭文件日志、仅控制台）：

```json
"logging": {
  "enabled": false
}
```

**示例**（仅文件、保留 7 天、单文件 128 MiB 滚动）：

```json
"logging": {
  "level": "info",
  "maxAgeDays": 7,
  "maxSizeMB": 128,
  "fileOnly": true
}
```

---

## 四、Tools 配置 `tools`

### 4.1 Web 工具

#### `tools.web.search.provider`

**含义**：网络搜索引擎。默认 DuckDuckGo，无需 API 密钥。

**类型**：`string`  
**可选值**：`duckduckgo`、`brave`、`tavily`、`jina`、`searxng`  
**默认值**：`duckduckgo`

**示例**（默认 DuckDuckGo，零配置，可省略 provider）：

```json
"search": { "maxResults": 5 }
```

**示例**（Tavily）：

```json
"provider": "tavily",
"apiKey": "tvly-xxx"
```

**示例**（自托管 SearXNG）：

```json
"provider": "searxng",
"baseUrl": "https://searx.example"
```

**环境变量**：`BRAVE_API_KEY`、`TAVILY_API_KEY`、`JINA_API_KEY`、`SEARXNG_BASE_URL`

---

#### `tools.web.search.apiKey`

**含义**：搜索 API 密钥。Brave/Tavily/Jina 需要；DuckDuckGo 不需要。

**类型**：`string`  
**获取**：Brave https://brave.com/search/api/；Tavily https://tavily.com；Jina https://jina.ai

---

#### `tools.web.search.baseUrl`

**含义**：SearXNG 实例地址（仅当 `provider` 为 `searxng` 时有效）。

**类型**：`string`  
**示例**：

```json
"baseUrl": "https://searx.example"
```

---

#### `tools.web.search.maxResults`

**含义**：`web_search` 返回的结果数量。

**类型**：`int`  
**默认值**：`5`  
**示例**（省 token）：

```json
"maxResults": 3
```

---

#### `tools.web.proxy`

**含义**：`web_search` 和 `web_fetch` 使用的 HTTP/SOCKS5 代理。

**类型**：`string`  
**示例**：

```json
"proxy": "http://127.0.0.1:7890"
```

或

```json
"proxy": "socks5://127.0.0.1:1080"
```

---

**Web 示例**：

（默认 DuckDuckGo，零配置）：

```json
"tools": {
  "web": {
    "search": { "maxResults": 5 }
  }
}
```

（Brave）：

```json
"tools": {
  "web": {
    "search": {
      "provider": "brave",
      "apiKey": "BSAxxxxxxxx",
      "maxResults": 5
    },
    "proxy": "http://127.0.0.1:7890"
  }
}
```

---

### 4.2 Exec 工具

#### `tools.exec.timeout`

**含义**：`exec` 工具执行命令的超时秒数。

**类型**：`int`  
**默认值**：`60`  
**示例**（长时间任务）：

```json
"timeout": 120
```

---

### 4.3 浏览器自动化（MCP）

通过 `tools.mcpServers` 接入官方包 [chrome-devtools-mcp](https://www.npmjs.com/package/chrome-devtools-mcp)（[GitHub](https://github.com/ChromeDevTools/chrome-devtools-mcp)）。**环境要求**（与 npm 说明一致）：Node.js **v20.19+**（或更新的维护版 LTS）、**npm**、**当前稳定版或更新的 Google Chrome**；官方仅保证 **Chrome / [Chrome for Testing](https://developer.chrome.com/blog/chrome-for-testing/)** 可用，其它 Chromium 系浏览器不保证。

> MCP 客户端**仅连接**到该服务器时**不会**自动启动浏览器；首次调用需要浏览器的工具时，服务器才会拉起或连接实例（见 npm 文档说明）。

**远程调试（`--autoConnect`）**：Chrome **M144+** 需在 `chrome://inspect/#remote-debugging` 启用远程调试（Edge 可用 `edge://inspect/#remote-debugging`）后，才能由 `--autoConnect` 自动匹配本机对应 `--channel` 用户数据目录下的已运行实例。

**常用 CLI 参数**（写在 `mcpServers.<name>.args` 中；完整列表以 `npx chrome-devtools-mcp@latest --help` 与 [npm「Configuration」](https://www.npmjs.com/package/chrome-devtools-mcp) 为准）：

| 参数 | 说明 |
|------|------|
| `--autoConnect` / `--auto-connect` | 自动连接本机已运行的 Chrome（M144+，需先启用远程调试）。默认 `false`。 |
| `--browserUrl` / `--browser-url` / `-u` | 连接已开启远程调试的实例，如 `http://127.0.0.1:9222`。 |
| `--wsEndpoint` / `--ws-endpoint` / `-w` | WebSocket 调试端点；与 `--browserUrl` 为不同连接方式。可从 `http://127.0.0.1:9222/json/version` 的 `webSocketDebuggerUrl` 取得。 |
| `--wsHeaders` / `--ws-headers` | 仅与 `--wsEndpoint` 配合，自定义 WebSocket 头（JSON 字符串）。 |
| `--headless` | 无界面模式，默认 `false`。 |
| `--executablePath` / `-e` | 自定义 Chrome 可执行文件路径。 |
| `--isolated` | 使用临时用户数据目录并在关闭后清理。 |
| `--userDataDir` / `--user-data-dir` | 指定用户数据目录；未指定时见官方默认路径说明。 |
| `--channel` | `stable`（默认）、`beta`、`dev`、`canary`。 |
| `--slim` | 仅暴露少量工具（导航、执行脚本、截图等），适合轻量场景。 |
| `--viewport` | 初始视口，如 `1280x720`；无头下单屏最大 3840×2160。 |
| `--proxyServer` / `--proxy-server` | 传给 Chrome 的代理参数。 |
| `--acceptInsecureCerts` | 忽略自签名/过期证书（慎用）。 |
| `--chromeArg` / `--chrome-arg` | 由 MCP 启动 Chrome 时的额外参数。 |
| `--categoryEmulation` / `--categoryPerformance` / `--categoryNetwork` / `--categoryExtensions` | 设为 `false`（或扩展类设为 `true`）可裁剪工具类别；扩展类与连接方式、Chrome 版本限制以官方文档为准。 |
| `--performanceCrux` | 设为 `false` 或传 **`--no-performance-crux`**：不向 CrUX 发送性能追踪 URL。 |
| `--usageStatistics` | 设为 `false` 或传 **`--no-usage-statistics`**：退出使用统计。 |
| `--logFile` / `--log-file` | 调试日志路径；可配合环境变量 `DEBUG=*` 输出详细日志。 |

**隐私与更新**：使用统计默认开启，可用 **`--no-usage-statistics`**，或设置环境变量 **`CHROME_DEVTOOLS_MCP_NO_USAGE_STATISTICS`** / **`CI`**（在 `mcpServers.<name>.env` 中配置）关闭。默认会检查 npm 是否有新版本并打日志，可设 **`CHROME_DEVTOOLS_MCP_NO_UPDATE_CHECKS`** 关闭检查。

**实验性功能**（按需，详见 npm）：`--experimentalVision`、`--experimentalScreencast`（需 [ffmpeg](https://www.ffmpeg.org/download.html) 在 PATH）、`--experimentalWebmcp`（Chrome 149+ 且需特定 Chrome 特性开关）等。

排错与工具列表：[troubleshooting](https://github.com/ChromeDevTools/chrome-devtools-mcp/blob/main/docs/troubleshooting.md)、[tool reference](https://github.com/ChromeDevTools/chrome-devtools-mcp/blob/main/docs/tool-reference.md)。

**示例**（`workspace/config.json` 的 `tools` 段）——与程序内 **`config.DefaultConfig()`** 一致：不预设浏览器附加模式，仅关闭 CrUX 上报与使用统计：

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

**`--browser-url`**：连接固定 CDP HTTP 地址（端口按你的环境修改）：

```json
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
```

轻量无头（与官方示例思路一致：`--slim` + `--headless`）：

```json
"tools": {
  "mcpServers": {
    "chrome-devtools": {
      "command": "npx",
      "args": ["-y", "chrome-devtools-mcp@latest", "--no-performance-crux", "--no-usage-statistics", "--slim", "--headless=true"]
    }
  }
}
```

默认不带 **`--browser-url`** / **`--auto-connect`**。请按场景在 `args` 中显式选择：**`--browser-url`**、**`--auto-connect`**（并完成远程调试）或 **`--ws-endpoint`**。更多场景见 npm 文档 [Connecting to a running Chrome instance](https://github.com/ChromeDevTools/chrome-devtools-mcp#connecting-to-a-running-chrome-instance)。

---

### 4.4 通用

#### `tools.restrictToWorkspace`

**含义**：是否限制 `exec` 和文件类工具仅在工作区内操作，提高安全性。

**类型**：`bool`  
**默认值**：`true`（省略该字段时与 `true` 相同）  
**示例**（显式关闭以允许工作区外路径时）：

```json
"restrictToWorkspace": true
```

---

### 4.5 MCP 服务器

#### `tools.mcpServers`

**含义**：MCP（Model Context Protocol）服务器列表，用于扩展工具能力。

**每个服务器配置**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `command` | string | 可执行命令（stdio 模式） |
| `args` | array | 命令参数 |
| `env` | object | 环境变量 |
| `url` | string | HTTP 模式时的服务 URL |
| `headers` | object | HTTP/SSE 自定义认证头，如 `{"Authorization": "Bearer xxx"}` |
| `toolTimeout` | int | 单次工具调用超时秒数，默认 30 |
| `enabledTools` | array | 仅注册指定工具；`["*"]` 表示全部；`[]` 表示不注册 |

**示例**：

```json
"mcpServers": {
  "filesystem": {
    "command": "npx",
    "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
  },
  "remote": {
    "url": "http://localhost:8000",
    "headers": {"Authorization": "Bearer xxx"},
    "toolTimeout": 60,
    "enabledTools": ["read_file", "write_file"]
  },
  "custom": {
    "command": "python",
    "args": ["-m", "my_mcp_server"],
    "env": {"API_KEY": "xxx"}
  }
}
```

---

## 五、Channels 配置 `channels`

各通道通用：`enabled`（是否启用）、`allowFrom`（用户 ID 白名单，空表示不限制）。

### 5.1 Telegram

| 参数 | 类型 | 说明 |
|------|------|------|
| `token` | string | Bot Token（来自 @BotFather） |
| `proxy` | string | HTTP/SOCKS5 代理，如 `http://127.0.0.1:7890` |

**示例**：

```json
"telegram": {
  "enabled": true,
  "token": "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz",
  "proxy": "socks5://127.0.0.1:1080",
  "allowFrom": []
}
```

---

### 5.2 WhatsApp

| 参数 | 类型 | 说明 |
|------|------|------|
| `bridgeUrl` | string | Bridge WebSocket 地址，默认 `ws://127.0.0.1:3001`。多实例时每个工作区需不同端口（如 `ws://127.0.0.1:3002`） |
| `bridgeToken` | string | Bridge 认证 Token |

**认证存储**：`workspace/whatsapp-auth`（按工作区隔离）。执行 `channels login --workspace PATH` 时使用对应工作区的 auth 目录。

**示例**：

```json
"whatsapp": {
  "enabled": true,
  "bridgeUrl": "ws://127.0.0.1:3001",
  "allowFrom": []
}
```

---

### 5.3 Discord

| 参数 | 类型 | 说明 |
|------|------|------|
| `token` | string | Bot Token |
| `gatewayUrl` | string | Gateway URL |
| `intents` | int | Intents 位掩码 |

**示例**：

```json
"discord": {
  "enabled": true,
  "token": "MTxxxxxxxx.xxxxxx.xxxxxxxx",
  "gatewayUrl": "wss://gateway.discord.gg/?v=10&encoding=json",
  "intents": 37377
}
```

---

### 5.4 Feishu（飞书）

| 参数 | 类型 | 说明 |
|------|------|------|
| `enabled` | bool | 是否启用 |
| `appId` | string | 飞书应用 ID |
| `appSecret` | string | 飞书应用密钥 |
| `encryptKey` | string | 事件加密密钥（可选） |
| `verificationToken` | string | 事件验证 Token（可选） |
| `allowFrom` | []string | 用户/群组白名单，空表示不限制 |

**示例**：

```json
"feishu": {
  "enabled": true,
  "appId": "xxx",
  "appSecret": "xxx",
  "allowFrom": []
}
```

---

### 5.5 DingTalk（钉钉）

| 参数 | 类型 | 说明 |
|------|------|------|
| `enabled` | bool | 是否启用 |
| `clientId` | string | 钉钉应用 Client ID |
| `clientSecret` | string | 钉钉应用 Client Secret |
| `allowFrom` | []string | 用户白名单，空表示不限制 |

**说明**：使用 Stream 模式。

**示例**：

```json
"dingtalk": {
  "enabled": true,
  "clientId": "xxx",
  "clientSecret": "xxx"
}
```

---

### 5.6 Email（邮件）

| 参数 | 类型 | 说明 |
|------|------|------|
| `enabled` | bool | 是否启用 |
| `consentGranted` | bool | 必须为 true 才能启用 |
| `imapHost` | string | IMAP 服务器地址 |
| `imapPort` | int | IMAP 端口 |
| `imapUsername` | string | IMAP 用户名 |
| `imapPassword` | string | IMAP 密码 |
| `imapMailbox` | string | 收件箱名称，如 `INBOX` |
| `imapUseSsl` | bool | 是否使用 SSL |
| `smtpHost` | string | SMTP 服务器地址 |
| `smtpPort` | int | SMTP 端口 |
| `smtpUsername` | string | SMTP 用户名 |
| `smtpPassword` | string | SMTP 密码 |
| `smtpUseTls` | bool | 是否使用 STARTTLS |
| `smtpUseSsl` | bool | 是否使用 SSL |
| `fromAddress` | string | 发件人地址 |
| `autoReplyEnabled` | bool | 是否自动回复 |
| `pollIntervalSeconds` | int | 轮询间隔（秒） |
| `markSeen` | bool | 是否标记已读 |
| `maxBodyChars` | int | 邮件正文最大字符数，0 表示默认 |
| `subjectPrefix` | string | 回复主题前缀 |
| `allowFrom` | []string | 发件人白名单，空表示不限制 |

**示例**：

```json
"email": {
  "enabled": true,
  "consentGranted": true,
  "imapHost": "imap.gmail.com",
  "imapPort": 993,
  "imapUseSsl": true,
  "smtpHost": "smtp.gmail.com",
  "smtpPort": 587,
  "smtpUseTls": true,
  "fromAddress": "bot@example.com",
  "autoReplyEnabled": true
}
```

---

### 5.7 Slack

| 参数 | 类型 | 说明 |
|------|------|------|
| `enabled` | bool | 是否启用 |
| `mode` | string | 模式，如 `socket` |
| `webhookPath` | string | Webhook 路径（webhook 模式） |
| `botToken` | string | Bot Token（xoxb-） |
| `appToken` | string | App Token（xapp-，Socket Mode） |
| `userTokenReadOnly` | bool | 用户 Token 是否只读 |
| `replyInThread` | bool | 是否在回复中回复 |
| `reactEmoji` | string | 收到消息时的反应 emoji |
| `allowFrom` | []string | 用户白名单 |
| `groupPolicy` | string | 群组策略 |
| `groupAllowFrom` | []string | 群组白名单 |
| `dm.enabled` | bool | 是否启用 DM |
| `dm.policy` | string | 策略：`open` 或 `allowlist` |
| `dm.allowFrom` | []string | DM 白名单 |

**示例**：

```json
"slack": {
  "enabled": true,
  "botToken": "xoxb-xxx",
  "appToken": "xapp-xxx",
  "mode": "socket",
  "replyInThread": true
}
```

---

### 5.8 Mochat（魔搭）

| 参数 | 类型 | 说明 |
|------|------|------|
| `enabled` | bool | 是否启用 |
| `baseUrl` | string | API 基础 URL |
| `socketUrl` | string | WebSocket URL |
| `socketPath` | string | WebSocket 路径 |
| `socketDisableMsgpack` | bool | 是否禁用 msgpack |
| `socketReconnectDelayMs` | int | 重连延迟（毫秒） |
| `socketMaxReconnectDelayMs` | int | 最大重连延迟 |
| `socketConnectTimeoutMs` | int | 连接超时 |
| `refreshIntervalMs` | int | 刷新间隔 |
| `watchTimeoutMs` | int | 监听超时 |
| `watchLimit` | int | 监听数量限制 |
| `retryDelayMs` | int | 重试延迟 |
| `maxRetryAttempts` | int | 最大重试次数 |
| `clawToken` | string | Claw 认证 Token |
| `agentUserId` | string | Agent 用户 ID |
| `sessions` | []string | 会话列表 |
| `panels` | []string | 面板列表 |
| `allowFrom` | []string | 用户白名单 |
| `mention.requireInGroups` | bool | 群组中是否必须 @ 才触发 |
| `groups` | object | 群组规则，如 `{"group1": {"requireMention": true}}` |
| `replyDelayMode` | string | 回复延迟模式 |
| `replyDelayMs` | int | 回复延迟（毫秒） |

---

### 5.9 QQ

| 参数 | 类型 | 说明 |
|------|------|------|
| `enabled` | bool | 是否启用 |
| `appId` | string | QQ 机器人 AppID |
| `secret` | string | QQ 机器人密钥 |
| `allowFrom` | []string | 用户白名单 |

**说明**：使用 botpy SDK，需配置 webhook 接收消息。

**示例**：

```json
"qq": {
  "enabled": true,
  "appId": "xxx",
  "secret": "xxx"
}
```

---

### 5.10 Wecom（企业微信）

| 参数 | 类型 | 说明 |
|------|------|------|
| `enabled` | bool | 是否启用 |
| `botId` | string | 企业微信智能机器人 Bot ID |
| `secret` | string | 机器人密钥 |
| `allowFrom` | []string | 用户白名单 |
| `welcomeMessage` | string | 进入会话时的欢迎语（可选） |

**说明**：使用 WebSocket 长连接（wss://openws.work.weixin.qq.com），无需公网 IP。

**示例**：

```json
"wecom": {
  "enabled": true,
  "botId": "xxx",
  "secret": "xxx",
  "allowFrom": []
}
```

---

### 5.11 Webhook（通道插件）

| 参数 | 类型 | 说明 |
|------|------|------|
| `enabled` | bool | 是否启用 |
| `port` | int | HTTP 服务端口，默认 9000 |
| `path` | string | POST 路径，默认 `/message` |
| `allowFrom` | []string | 发送者白名单，`["*"]` 表示允许所有 |
| `rateLimitPerMinute` | int | 每分钟请求限流，默认 `120` |
| `rateLimitIPv4Prefix` | int | IPv4 聚合前缀，默认 `32` |
| `rateLimitIPv6Prefix` | int | IPv6 聚合前缀，默认 `128` |
| `rateLimitCidrs` | []string | 自定义 CIDR 限流桶 |

**说明**：插件式通道，外部服务通过 HTTP POST 注入消息到 bus。支持自定义通道或自动化脚本。

**请求体**：`{"sender":"id","chat_id":"id","text":"消息内容","media":[]}`

**示例**：

```json
"webhook": {
  "enabled": true,
  "port": 9000,
  "path": "/message",
  "allowFrom": ["*"]
}
```

**测试**：

```bash
curl -X POST http://localhost:9000/message \
  -H "Content-Type: application/json" \
  -d '{"sender":"user1","chat_id":"user1","text":"Hello!"}'
```

---

### 5.12 Channels 配置总览

```json
"channels": {
  "whatsapp": { "enabled": false, "bridgeUrl": "ws://127.0.0.1:3001" },
  "telegram": { "enabled": false },
  "discord": { "enabled": false, "gatewayUrl": "wss://gateway.discord.gg/?v=10&encoding=json", "intents": 37377 },
  "feishu": { "enabled": false },
  "mochat": { "enabled": false },
  "dingtalk": { "enabled": false },
  "email": { "enabled": false, "consentGranted": false },
  "slack": { "enabled": false, "dm": { "enabled": false } },
  "qq": { "enabled": false },
  "wecom": { "enabled": false },
  "webhook": { "enabled": false, "port": 9000 }
}
```

---

## 六、最小配置示例

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
## 七、完整配置示例

### 7.1 省 Token 配置

```json
{
  "agents": {
    "defaults": {
      "workspace": "~/.dipper-bot/workspace",
      "model": "anthropic/claude-3-5-haiku",
      "maxTokens": 4096,
      "temperature": 0.7,
      "memoryWindow": 24,
      "maxToolIterations": 15,
      "runTimeoutSec": 300,
      "lcm": {
        "enabled": true,
        "freshTailCount": 20,
        "leafTargetTokens": 1000,
        "condensedTargetTokens": 1500
      },
    }
  },
  "providers": {
    "custom": {
      "apiKey": "your-key",
      "apiBase": "https://api.openai.com/v1"
    }
  },
  "gateway": {
    "host": "0.0.0.0",
    "port": 8090
  },
  "tools": {
    "web": {
      "search": { "apiKey": "BSAxxx", "maxResults": 3 }
    }
  }
}
```

### 7.2 长对话 + 多工具配置

```json
{
  "agents": {
    "defaults": {
      "model": "gpt-4o",
      "maxTokens": 8192,
      "memoryWindow": 50,
      "maxToolIterations": 25,
      "lcm": {
        "enabled": true,
        "freshTailCount": 32,
        "leafTargetTokens": 1200,
        "condensedTargetTokens": 2000
      }
    }
  },
  "tools": {
    "restrictToWorkspace": true,
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

### 7.3 带语音转写的 Web 配置

```json
{
  "agents": {
    "defaults": {
      "model": "gpt-4o-mini",
      "maxTokens": 4096
    }
  },
  "providers": {
    "custom": {
      "apiKey": "sk-xxx",
      "apiBase": "https://api.openai.com/v1"
    },
    "transcription": { "provider": "groq" },
    "groq": { "apiKey": "gsk_xxx" }
  }
}
```

---

## 八、工作区文件对 Token 的影响

以下文件会作为系统提示的一部分加载，体积越大 token 越多：

| 文件 | 说明 |
|------|------|
| `AGENTS.md` | 行为与记忆规则 |
| `SOUL.md` | 人格设定 |
| `USER.md` | 用户信息 |
| `TOOLS.md` | 工具说明 |
| `IDENTITY.md` | 身份描述 |
| `memory/MEMORY.md` | 长期记忆 |
| `memory/USER.md` | 用户偏好与长期资料（memory tool 维护） |
| `memory/NOTE.md` | Agent 记忆笔记（memory tool 维护） |

**建议**：保持精简，定期清理 `MEMORY.md` 中的冗余内容。

---

## 九、常见问题

**Q: 配置修改后不生效？**  
A: 确认修改的是 `workspace/config.json`，且未通过 `--workspace` 指定其他路径。

**Q: 如何切换模型？**  
A: 修改 `agents.defaults.model`。OpenRouter 格式：`anthropic/claude-3-5-sonnet`；OpenAI Codex：`openai-codex/gpt-5.1-codex`。

**Q: LCM 启用后会话变慢？**  
A: LCM 会调用 LLM 做摘要，首次或长对话时会有额外延迟。可适当降低 `leafTargetTokens`、`condensedTargetTokens`。

**Q: 如何减少 token 消耗？**  
A: 1) 启用 LCM；2) 降低 `memoryWindow`；3) 降低 `maxTokens`；4) 精简工作区 bootstrap 文件；5) 降低 `web.search.maxResults`。

