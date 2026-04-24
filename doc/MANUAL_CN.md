# dipper-bot 操作手册

🐕 **dipper-bot** 是一款超轻量级个人 AI 助手，支持 OpenAI 兼容 API、消息通道、定时任务、MCP 扩展工具等。

**语言**：本文为中文版；英文版见 [MANUAL.md](./MANUAL.md)。**完整 `config.json` 字段、默认值与示例**见 [CONFIG_CN.md](./CONFIG_CN.md)。**从源码构建、目录结构、测试与部署**见 [BUILD_CN.md](./BUILD_CN.md)。

**交互式输入**：支持 Backspace 删除、方向键移动、Ctrl+U/K 等行编辑，以及上下键历史记录。**运行取消**：同会话新消息会中止前一次执行；可配置超时。

**交互式 REPL**：无参数启动（或 Windows 双击 exe）时，显示帮助并进入命令提示符，可输入 `agent`、`status` 等命令；输入 `help` 或 `?` 重新打印帮助；输入 `exit`、`quit` 或 `:q` 退出。

---

## 1. 快速开始

### 1.1 初始化

**打开终端：**
- **macOS**：打开「终端」应用（应用程序 → 实用工具 → 终端，或 Spotlight 搜索「终端」）
- **Linux**：按 `Ctrl+Alt+T` 或从应用菜单打开「终端」
- **Windows**：按 `Win+X` 选择「Windows PowerShell」，或开始菜单搜索「PowerShell」。若 emoji 显示为 ◆，建议使用 [Windows Terminal](https://learn.microsoft.com/zh-cn/windows/terminal/install)

**若程序在桌面：** 打开终端后，先进入桌面目录再执行命令。
- **macOS / Linux**：`cd ~/Desktop` 后执行 `./dipper-bot onboard` 等
- **Windows**：`cd $env:USERPROFILE\Desktop` 后执行 `.\dipper-bot.exe onboard` 等（或右键桌面 →「在终端中打开」/「Open in Terminal」，部分系统支持）


**macOS / Linux：**
```bash
./dipper-bot onboard
```

**Windows：**
```powershell
.\dipper-bot.exe onboard
```

初始化时会提示输入工作区路径，可直接回车使用默认 `~/.dipper-bot/workspace`，或输入自定义路径（如 `~/projects/my-agent`）。

### 1.2 配置 API 密钥

编辑 `workspace/config.json`（或工作区内的 `config.json`），添加：

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

- `apiKey`：必填，API 密钥
- `apiBase`：可选，默认 `https://api.openai.com/v1`，可改为 OpenRouter、本地部署等 OpenAI 兼容地址

### 1.3 开始对话

**macOS / Linux：**
```bash
# 单次对话
./dipper-bot agent -m "你好！"

# 交互式对话（支持行编辑与历史）
./dipper-bot agent
```

**Windows：**
```powershell
# 单次对话
.\dipper-bot.exe agent -m "你好！"

# 交互式对话（支持行编辑与历史）
.\dipper-bot.exe agent
```

**帮助命令**：`.\dipper-bot.exe help` 或 `.\dipper-bot.exe -h` 打印使用说明。交互式 REPL 内输入 `help` 或 `?` 亦可。

---

## 2. 命令详解

### 2.1 onboard — 初始化

**macOS / Linux：**
```bash
./dipper-bot onboard [--workspace PATH] [-w PATH]
```

**Windows：**
```powershell
.\dipper-bot.exe onboard [--workspace PATH] [-w PATH]
```

- 创建配置与工作区目录
- 工作区路径写入 `~/.dipper-bot/config.json`
- 完整配置写入 `workspace/config.json`
- 支持 `--workspace` / `-w` 指定工作区

### 2.2 agent — 对话

**macOS / Linux：**
```bash
./dipper-bot agent [--workspace PATH] [-m MSG] [--markdown|--no-markdown] [--logs|--no-logs] [--web [--host HOST] [-p PORT]] [-s SESSION]
```

**Windows：**
```powershell
.\dipper-bot.exe agent [--workspace PATH] [-m MSG] [--markdown|--no-markdown] [--logs|--no-logs] [--web [--host HOST] [-p PORT]] [-s SESSION]
```

| 参数 | 说明 |
|------|------|
| `-m`, `--message` | 单次消息，不进入交互模式 |
| `--markdown` | 输出 Markdown（默认） |
| `--no-markdown` | 纯文本输出 |
| `--logs` | 显示工具调用日志 |
| `--no-logs` | 不显示日志（默认） |
| `--web` | 启动网页版对话界面 |
| `--host` | 绑定地址（如 127.0.0.1 仅本机，0.0.0.0 所有接口；可配合 `--web` 或 gateway） |
| `-p`, `--port` | 网页版端口（默认 8600，配合 `--web`） |
| `-s`, `--session` | 会话标识，如 `cli:direct` |
| `--workspace` | 指定工作区，覆盖默认配置 |

**网页版**：`dipper-bot agent --web` 启动后，在浏览器打开 `http://localhost:8600` 即可进行对话。支持 `/new` 新会话、`/help` 帮助。

**网页版 API**：
- **GET /** — 聊天页面
- **POST /api/chat** — 发送消息。请求体 `{"content": "你好"}`，返回 `{"content": "..."}` 或 `{"error": "..."}`
- **POST /api/upload** — 上传文件（multipart/form-data，字段 `file`）。文件保存到 `workspace/uploads/`，返回 `{"paths": ["uploads/文件名.ext"]}`
- **POST /api/transcribe** — 语音转写（multipart 字段 `audio`，WebM 格式）。返回 `{"text": "..."}`。需配置 `providers.transcription`（见 4.1 节）

**网页版选项**：`--host` 绑定地址（默认从 `gateway.host` 或 `0.0.0.0`），`-p` / `--port` 端口（默认 8600）。会话固定为 `web:default`。

**交互式模式快捷键**：Backspace 删除、方向键移动、Ctrl-A/E 行首/尾、Ctrl-U/K 删至行首/尾、上下键历史记录、Ctrl+C 清空、输入 `exit` 或 `:q` 退出。

### 2.3 gateway — 网关服务

**macOS / Linux：**
```bash
./dipper-bot gateway [--workspace PATH] [--host HOST] [-p PORT] [-v|--verbose]
```

**Windows：**
```powershell
.\dipper-bot.exe gateway [--workspace PATH] [--host HOST] [-p PORT] [-v|--verbose]
```

- 启动 HTTP 网关与 Agent 循环
- 默认端口 `8090`
- `--host` 绑定地址（如 127.0.0.1、0.0.0.0；也可在 config.json 的 `gateway.host` 配置）
- `-p` / `--port` 指定端口
- `-v` / `--verbose` 开启调试日志

### 2.4 status — 状态查看

**macOS / Linux：**
```bash
./dipper-bot status [--workspace PATH]
```

**Windows：**
```powershell
.\dipper-bot.exe status [--workspace PATH]
```

显示配置路径、工作区、模型、API 密钥状态。

### 2.5 provider — 提供商登录

**macOS / Linux：**
```bash
./dipper-bot provider login openai-codex    # OpenAI Codex（OAuth）
./dipper-bot provider login github-copilot # GitHub Copilot（OAuth）
```

**Windows：**
```powershell
.\dipper-bot.exe provider login openai-codex
.\dipper-bot.exe provider login github-copilot
```

### 2.6 channels — 通道管理

**macOS / Linux：**
```bash
./dipper-bot channels status [--workspace PATH]   # 查看通道状态
./dipper-bot channels login [--workspace PATH]    # WhatsApp 扫码登录
```

**Windows：**
```powershell
.\dipper-bot.exe channels status [--workspace PATH]   # 查看通道状态
.\dipper-bot.exe channels login [--workspace PATH]    # WhatsApp 扫码登录
```

### 2.7 cron — 定时任务

**macOS / Linux：**
```bash
./dipper-bot cron list [-a]                    # 列出任务（-a 含禁用）
./dipper-bot cron add -n NAME -m MSG [选项]    # 添加任务
./dipper-bot cron remove ID                    # 删除任务
./dipper-bot cron run ID [-f]                  # 立即执行一次
./dipper-bot cron enable ID [--disable]        # 启用/禁用
./dipper-bot cron disable ID                   # 禁用
```

**Windows：**
```powershell
.\dipper-bot.exe cron list [-a]                    # 列出任务（-a 含禁用）
.\dipper-bot.exe cron add -n NAME -m MSG [选项]    # 添加任务
.\dipper-bot.exe cron remove ID                    # 删除任务
.\dipper-bot.exe cron run ID [-f]                  # 立即执行一次
.\dipper-bot.exe cron enable ID [--disable]        # 启用/禁用
.\dipper-bot.exe cron disable ID                   # 禁用
```

**添加任务示例：**

**macOS / Linux：**
```bash
# 每 3600 秒执行一次
./dipper-bot cron add -n "hourly" -m "检查状态" -e 3600

# 使用 cron 表达式（每天 9 点）
./dipper-bot cron add -n "daily" -m "早安！" -c "0 9 * * *" --tz "Asia/Shanghai"

# 一次性任务
./dipper-bot cron add -n "meeting" -m "会议开始了" --at "2025-03-10T15:00:00"

# 带交付到通道
./dipper-bot cron add -n "reminder" -m "提醒内容" -e 3600 -d
```

**Windows：**
```powershell
.\dipper-bot.exe cron add -n "hourly" -m "检查状态" -e 3600
.\dipper-bot.exe cron add -n "daily" -m "早安！" -c "0 9 * * *" --tz "Asia/Shanghai"
.\dipper-bot.exe cron add -n "meeting" -m "会议开始了" --at "2025-03-10T15:00:00"
.\dipper-bot.exe cron add -n "reminder" -m "提醒内容" -e 3600 -d
```

| 参数 | 说明 |
|------|------|
| `-n`, `--name` | 任务名称 |
| `-m`, `--message` | 发送给 Agent 的消息 |
| `-e`, `--every` | 间隔秒数 |
| `-c`, `--cron` | cron 表达式 |
| `--tz` | 时区（配合 `-c`） |
| `--at` | 一次性执行时间（RFC3339） |
| `-d`, `--deliver` | 将结果交付到通道 |

---

## 3. 配置说明

### 3.1 配置结构

- **工作区路径**：`~/.dipper-bot/config.json` 的 `agents.defaults.workspace`
- **其他配置**：`workspace/config.json`（providers、channels、gateway 等）

### 3.2 配置优先级

- 工作区配置（`workspace/config.json`）优先于 `~/.dipper-bot/config.json`
- 无 `--workspace` 时，从 `~/.dipper-bot/config.json` 读取工作区路径
- 有 `--workspace` 时，优先使用该工作区的配置

### 3.3 环境变量

- `DIPPER_WORKSPACE`：在未传 `--workspace` 时作为工作区路径（如 `status`、`agent`）

### 3.4 多实例（多工作区）

| 资源 | 存储位置 | 说明 |
|------|----------|------|
| Cron | `workspace/cron/jobs.json` | 每个工作区独立定时任务 |
| WhatsApp 认证 | `workspace/whatsapp-auth` | 每个工作区独立扫码；多实例需配置不同 `bridgeUrl` 端口 |

**运行多 Gateway**：
```bash
# 终端 1：工作区 A，端口 8090
./dipper-bot gateway --workspace ~/workspace-a -p 8090

# 终端 2：工作区 B，端口 8091
./dipper-bot gateway --workspace ~/workspace-b -p 8091
```

**通道**：每个工作区使用各自的 `config.json`。Telegram/Discord 需不同 bot token；WhatsApp 需为每个工作区配置不同 `bridgeUrl`（如 `ws://127.0.0.1:3002`），并分别执行 `channels login --workspace PATH` 扫码。

### 3.5 Token-based memory（MemoryConsolidator，可选）

在 `agents.defaults` 中设置 `contextWindowTokens` > 0 启用。**MemoryConsolidator**：从对话中抽取事实和事件 → MEMORY.md + HISTORY.md。当预估 prompt 超过窗口时，将旧消息归档到 `memory/MEMORY.md` 和 `memory/HISTORY.md`，直到 prompt 低于一半窗口。

**职责区分**：MemoryConsolidator 抽取事实与事件；LCM 压缩对话历史并组装上下文。二者可单独或同时启用。

```json
"contextWindowTokens": 131072
```

### 3.6 LCM 无损上下文（可选）

**LCM**：压缩对话历史 → DAG 摘要 → 组装对话上下文。在 `agents.defaults.lcm` 中启用，替代滑动窗口截断，实现 DAG 摘要式长对话上下文：

```json
"lcm": {
  "enabled": true,
  "contextThreshold": 0.60,
  "freshTailCount": 16,
  "leafMinFanout": 10,
  "condensedMinFanout": 8,
  "leafChunkTokens": 12000,
  "leafTargetTokens": 600,
  "condensedTargetTokens": 900,
  "incrementalMaxDepth": 1
}
```

启用后 Agent 可使用 `lcm_grep`、`lcm_describe` 检索压缩历史。数据存储在 `workspace/lcm.db`。

### 3.7 记忆维护后台流程（默认启用）

学习由双后台流程驱动：`memoryMaintenance`（记忆维护）与 `skillsEvolution`（技能自演化，含可选执行期反思）。  
记忆维护仅通过 `memory` 工具更新 `memory/USER.md` 与 `memory/NOTE.md`；技能演化会创建/修补 `skills/*/SKILL.md`。  
用户侧反馈默认即时第二条消息（`agents.defaults.experience.learnerFeedbackInstantPush=true`）；设为 `false` 时改为下一条回复前置 digest。

### 3.8 完整配置示例

```json
{
  "agents": {
    "defaults": {
      "workspace": "~/.dipper-bot/workspace",
      "model": "gpt-4",
      "maxTokens": 8192,
      "temperature": 0.7
    }
  },
  "providers": {
    "custom": {
      "apiKey": "sk-xxx",
      "apiBase": "https://api.openai.com/v1"
    }
  },
  "gateway": {
    "host": "0.0.0.0",
    "port": 8090
  }
}
```

**可选配置**：
- `agents.defaults.contextWindowTokens`：Token-based memory（MemoryConsolidator），>0 时启用，将旧对话归档到 MEMORY.md + HISTORY.md
- `agents.defaults.runTimeoutSec`：单次对话最大执行秒数，0 表示不限制；同会话新消息会中止前一次执行
- 后台记忆维护：由 AgentLoop 内置维护器异步执行，无需额外配置项
- `tools.web.search`：网络搜索。`provider`：duckduckgo（默认，零配置）、brave、tavily、jina、searxng。`apiKey` 用于 Brave/Tavily/Jina；`baseUrl` 用于 SearXNG。环境变量：`BRAVE_API_KEY`、`TAVILY_API_KEY`、`JINA_API_KEY`、`SEARXNG_BASE_URL`
- `tools.web.proxy`：web_search、web_fetch 的 HTTP/SOCKS5 代理
- `providers.groq.apiKey`：Groq API key（当 `transcription.provider` 为 groq 时用于语音转写）
- `providers.transcription`：语音转写配置。`provider`: `"groq"`（云端）或 `"vosk"`（本地）；`voskUrl`：Vosk WebSocket 地址（默认 `ws://localhost:2700`）。需先启动 Vosk 服务：`docker run -d -p 2700:2700 alphacep/kaldi-cn`（中文）或 `alphacep/kaldi-en`（英文）。需安装 ffmpeg。
- `agents.defaults.reasoningEffort`：思考模式（o1、o3、Codex）：`"low"` / `"medium"` / `"high"`

---

## 4. Agent 网页版（agent --web）

`dipper-bot agent --web` 启动网页对话界面，默认端口 8600。在浏览器打开 `http://localhost:8600` 即可对话。

| 接口 | 说明 |
|------|------|
| GET / | 聊天页面（HTML） |
| POST /api/chat | 发送消息，请求体 `{"content": "消息内容"}`，返回 `{"content": "回复"}` 或 `{"error": "错误"}` |
| POST /api/upload | 上传文件，multipart 字段 `file`，文件保存到 `workspace/uploads/`，返回 `{"paths": ["uploads/xxx"]}` |
| POST /api/transcribe | 语音转写，multipart 字段 `audio`（WebM），返回 `{"text": "..."}`。需配置 `providers.transcription` |

斜杠命令：`/new` 新会话、`/help` 帮助。会话 ID 为 `web:default`。支持麦克风语音输入（按钮 🎤）。

### 4.1 语音输入（Vosk 本地转写）

网页版和 Telegram 支持语音转文字。可使用 **Groq**（云端）或 **Vosk**（本地，完全离线）。

**使用 Vosk 本地转写：**

1. **安装 ffmpeg**（用于音频格式转换）：
   - macOS：`brew install ffmpeg`
   - Ubuntu/Debian：`sudo apt install ffmpeg`
   - Windows：从 [ffmpeg.org](https://ffmpeg.org/download.html) 下载或 `winget install ffmpeg`

2. **启动 Vosk 服务**（Docker）：
   ```bash
   # 中文模型
   docker run -d -p 2700:2700 alphacep/kaldi-cn

   # 英文模型
   docker run -d -p 2700:2700 alphacep/kaldi-en
   ```

3. **配置 config.json**：
   ```json
   "providers": {
     "transcription": {
       "provider": "vosk",
       "voskUrl": "ws://localhost:2700"
     }
   }
   ```

4. **使用**：
   - **网页版**：点击输入框旁的 🎤 按钮，开始录音，再次点击停止，转写结果自动填入输入框
   - **Telegram**：发送语音消息，自动转写为文字后发送给 Agent

**使用 Groq 云端转写**（需 API Key）：
   ```json
   "providers": {
     "groq": { "apiKey": "your-groq-api-key" },
     "transcription": { "provider": "groq" }
   }
   ```

---

## 5. Gateway API

网关运行后，可通过 HTTP 接口发送消息：

**POST /message**

```json
{
  "channel": "web",
  "chat_id": "user1",
  "sender_id": "user",
  "content": "Hello"
}
```

返回 202 Accepted，消息由 Agent 异步处理。

---

## 6. 通道集成

### 6.1 Telegram

在 `workspace/config.json` 中配置：

```json
"channels": {
  "telegram": {
    "enabled": true,
    "token": "Bot Token from @BotFather",
    "allowFrom": ["user_id1", "user_id2"]
  }
}
```

### 6.2 WhatsApp

1. 设置 `channels.whatsapp.enabled: true`
2. 运行 `./dipper-bot channels login`（macOS/Linux）或 `.\dipper-bot.exe channels login`（Windows）扫码绑定
3. Bridge 已提供，见 README

### 6.3 Feishu（飞书）

```json
"channels": {
  "feishu": {
    "enabled": true,
    "appId": "xxx",
    "appSecret": "xxx",
    "allowFrom": []
  }
}
```

需配置事件订阅以接收消息。

### 6.4 DingTalk（钉钉）

```json
"channels": {
  "dingtalk": {
    "enabled": true,
    "clientId": "xxx",
    "clientSecret": "xxx",
    "allowFrom": []
  }
}
```

使用 Stream 模式。

### 6.5 Discord

```json
"channels": {
  "discord": {
    "enabled": true,
    "token": "Bot Token",
    "allowFrom": []
  }
}
```

### 6.6 Slack、Email、QQ

- **Slack**：`channels.slack.enabled`、`botToken`、`appToken`，Socket Mode
- **Email**：`channels.email.enabled`、`consentGranted: true`，IMAP/SMTP 凭证
- **QQ**：`channels.qq.enabled`、`appId`、`secret`，需配置 Webhook

---

## 7. 工作区结构

```
workspace/
├── config.json      # 工作区配置
├── AGENTS.md        # Agent 指令
├── SOUL.md          # 人格设定
├── USER.md          # 用户信息
├── HEARTBEAT.md     # 定期任务（每 30 分钟检查）
├── memory/          # 记忆（MemoryConsolidator 归档事实与事件）
│   ├── MEMORY.md    # 长期记忆（进入系统提示词）
│   ├── HISTORY.md   # 事件日志（grep 检索）
│   ├── USER.md      # 用户偏好与长期资料（memory tool 维护）
│   └── NOTE.md      # Agent 记忆笔记（memory tool 维护）
├── skills/          # 技能
├── sessions/        # 会话持久化（JSONL）
└── lcm.db           # LCM 无损上下文（启用时）
```

---

## 8. Agent 工具

| 工具 | 说明 |
|------|------|
| read_file | 读取文件 |
| write_file | 写入文件 |
| edit_file | 查找替换编辑 |
| list_dir | 列出目录 |
| exec | 执行 shell 命令（支持 Python venv） |
| message | 发送消息到通道 |
| cron | 添加/列出/删除定时任务 |
| spawn | 后台子 Agent |
| web_search | 网络搜索（多引擎：duckduckgo 默认、brave、tavily、jina、searxng） |
| web_fetch | 抓取 URL 内容 |
| sessions_list | 列出所有会话 |
| sessions_history | 获取指定会话的消息历史 |
| sessions_send | 向另一会话发送消息 |
| lcm_grep | 按正则搜索 LCM 压缩历史（仅 LCM 启用时） |
| lcm_describe | 获取压缩历史的高层描述（仅 LCM 启用时） |
| MCP_* | MCP 服务器注册的工具 |

### 8.1 Chrome DevTools MCP

[Chrome DevTools MCP](https://github.com/ChromeDevTools/chrome-devtools-mcp) 默认不预设附加模式，仅带上 **`--no-performance-crux`**、**`--no-usage-statistics`**。可按需把 `args` 改成 **`--browser-url=...`** 或 **`--auto-connect`**（附加本机已启用远程调试的 Chrome）。

默认生成的 `workspace/config.json` 已包含：

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

**远程调试**：Chrome M144+（Beta 或 Stable）。先在 Chrome 中启用远程调试：打开 `chrome://inspect/#remote-debugging`（Edge 可用 `edge://inspect/#remote-debugging`），按提示操作。

在 `tools.mcpServers` 中添加：

```json
"mcpServers": {
  "chrome-devtools": {
    "command": "npx",
    "args": ["chrome-devtools-mcp@latest", "--autoConnect", "--channel=beta"]
  }
}
```

Chrome M144 进入稳定版后，可改用 `--channel=stable`。Agent 连接时 Chrome 会弹出权限对话框，点击**允许**即可。连接期间会显示「Chrome 正受到自动化测试软件控制」横幅。

**说明**：请通过 `tools.mcpServers` 接入 Chrome DevTools MCP。
`chrome-devtools-mcp` 可附加到当前 live 会话，复用 cookies 和登录状态，适合调试和已登录场景。

### 8.2 会话工具（OpenClaw 风格）

| 工具 | 说明 |
|------|------|
| sessions_list | 列出所有会话，返回 key、channel、chatId、updatedAt、msgCount |
| sessions_history | 获取指定 sessionKey 的消息历史 |
| sessions_send | 向另一会话发送消息，触发 Agent 处理并回复到该会话 |

使用流程：先 `sessions_list` 获取会话列表，再用 `sessions_history` 查看历史，或用 `sessions_send` 向其他会话发消息。

---

## 9. 常见问题

### Q: 提示 "no API key"

在 `workspace/config.json` 中设置 `providers.custom.apiKey`。若使用自定义工作区，确保该工作区的 `config.json` 存在且包含正确配置。

### Q: 如何切换工作区

- 使用 `--workspace PATH` 参数
- 或设置环境变量 `DIPPER_WORKSPACE`
- 或修改 `~/.dipper-bot/config.json` 中的 `agents.defaults.workspace`

### Q: 交互式模式斜杠命令

- `/new` — 开始新会话
- `/help` — 显示命令帮助

### Q: 新消息会中止前一次执行吗？

网关模式下，同一会话（channel:chat_id）收到新消息时，会取消前一次正在执行的对话，只处理最新一条。可配置 `agents.defaults.runTimeoutSec` 限制单次执行时长（秒），超时后自动取消并返回 "Request timed out."。

### Q: MemoryConsolidator 和 LCM 有什么区别？

- **MemoryConsolidator**（`contextWindowTokens` > 0）：从对话中抽取事实和事件，写入 MEMORY.md + HISTORY.md
- **LCM**（`lcm.enabled`）：压缩对话历史为 DAG 摘要，组装对话上下文。二者职责不同，可单独或同时启用

### Q: 现在的学习机制是什么？

- 学习由 `memoryMaintenance` + `skillsEvolution` 组成：前者维护 `memory/USER.md`/`memory/NOTE.md`，后者创建或修补 `skills/*/SKILL.md`
- `skillsEvolution.midRunReflectEveryToolIters` / `midRunReflectMinSeconds` 可控制执行期技能反思频率与冷却
- 异步学习反馈默认即时第二条消息；将 `agents.defaults.experience.learnerFeedbackInstantPush` 设为 `false` 时，改为下一条回复前置 digest（互斥不重复）

### Q: 如何添加 MCP 工具

在 `workspace/config.json` 的 `tools.mcpServers` 中配置：

```json
"mcpServers": {
  "filesystem": {
    "command": "npx",
    "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
  },
  "remote": {
    "url": "http://localhost:8000",
    "headers": { "Authorization": "Bearer xxx" },
    "toolTimeout": 60,
    "enabledTools": ["*"]
  },
  "chrome-devtools": {
    "command": "npx",
    "args": ["chrome-devtools-mcp@latest", "--autoConnect", "--channel=beta"]
  }
}
```

- `headers`：自定义认证头（HTTP MCP）
- `toolTimeout`：工具调用超时秒数（默认 30）
- `enabledTools`：`["*"]` 全部启用，或指定工具名列表；`[]` 禁用
- `chrome-devtools` 为 attach 模式，可附加到已登录的 Chrome 会话（见 8.1 节）

---

## 10. 版本

**macOS / Linux：**
```bash
./dipper-bot -v
# 或
./dipper-bot --version
```

**Windows：**
```powershell
.\dipper-bot.exe -v
# 或
.\dipper-bot.exe --version
```
