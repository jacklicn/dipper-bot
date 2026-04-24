# dipper-bot

🐕 **dipper-bot** — 用 **Go** 编写的轻量、可自托管的个人 AI 助手：单进程内包含网关、Agent 循环、工具集与可选 Web 聊天界面。同一会话收到新消息时会取消上一轮执行；可配置单次运行超时。

## 功能概览

- **网关 + Agent** — HTTP `POST /message`、进程内消息总线、兼容 OpenAI 的 LLM 调用（另支持可选的 Codex / GitHub Copilot OAuth）。
- **工具** — 读/写/改文件、列目录、Shell、`message`、定时 `cron` 与子任务 `spawn`、联网搜索与抓取、CDP 浏览器自动化、**MCP** 服务、会话列表/历史/发送、**LCM** 工具（`lcm_grep`、`lcm_describe` 等）。
- **上下文** — 可选 **按 token 的记忆整理**（MemoryConsolidator → `MEMORY.md` / `HISTORY.md`）；**LCM**（DAG + SQLite，偏无损式召回）**默认开启**（`agents.defaults.lcm.enabled` 设为 `false` 可关闭）。
- **精选记忆** — 面向 `USER.md`、`NOTE.md` 的 `memory` / `session_search`（含 FTS）；后台记忆维护钩子。
- **学习闭环** — 自主 `memoryMaintenance` + `skillsEvolution`（含可选执行期技能反思）；学习反馈默认即时第二条消息（`experience.learnerFeedbackInstantPush=true`，设为 `false` 则改为下一条回复 digest）。
- **通道** — Telegram、WhatsApp（Baileys 桥）、Discord、飞书、钉钉、Slack、邮件、QQ、企业微信、Webhook（详见手册）。
- **Web 聊天** — 内置浏览器聊天界面（`agent --web`，默认 `http://localhost:8600`）。
- **安全防护** — Gateway/Webhook 入站启用严格 JSON 解析与请求体大小限制；Web 抓取默认拦截私网/回环地址（SSRF 防护）。
- **其它** — 心跳、按工作区的定时任务。

## 快速开始

```bash
go build -o dipper-bot .
./dipper-bot onboard
# 在 workspace/config.json 中设置 providers.custom.apiKey
./dipper-bot agent -m "Hello!"
./dipper-bot agent --web    # 推荐：浏览器内 Web 聊天（默认 http://localhost:8600）
./dipper-bot gateway --port 8090
```

**Windows：** `go build -o dipper-bot.exe .`，然后使用 `.\dipper-bot.exe …`（参数相同，例如 `.\dipper-bot.exe agent --web`）。

## 文档

默认以 **英文** 撰写（`*.md`）。中文对照在同一目录下，文件名为 **`*_CN.md`**。

| 英文 | 中文 | 主题 |
|------|------|------|
| [README.md](./README.md) | **本页（README_CN.md）** | 项目简介与快速开始 |
| [doc/MANUAL.md](./doc/MANUAL.md) | [doc/MANUAL_CN.md](./doc/MANUAL_CN.md) | 安装、CLI、通道、网关 / Web UI |
| [doc/CONFIG.md](./doc/CONFIG.md) | [doc/CONFIG_CN.md](./doc/CONFIG_CN.md) | `config.json`（英文为摘要；中文为完整字段说明） |
| [doc/BUILD.md](./doc/BUILD.md) | [doc/BUILD_CN.md](./doc/BUILD_CN.md) | 构建、测试、仓库结构、部署指引 |
| [doc/DESIGN.md](./doc/DESIGN.md) | [doc/DESIGN_CN.md](./doc/DESIGN_CN.md) | 架构设计 |
| [doc/LCM.md](./doc/LCM.md) | [doc/LCM_CN.md](./doc/LCM_CN.md) | 无损上下文（LCM） |
| [doc/PRIVACY.md](./doc/PRIVACY.md) | [doc/PRIVACY_CN.md](./doc/PRIVACY_CN.md) | 隐私与数据流 |
| [deploy/README.md](./deploy/README.md) | — | Docker、systemd、Windows 服务 |

## 与同类项目的差异

| | **dipper-bot** | **OpenClaw** | **Hermes Agent** |
|---|----------------|--------------|------------------|
| **形态** | 核心循环与网关合一的 **Go** 二进制；仅 WhatsApp 依赖可选的小型 **Node** 桥。 | **Node/TypeScript** 平台，**插件**生态丰富（含 LCM 类插件如 lossless-claw）。 | **多服务**研究型 Agent（安装器、网关、大量通道与模型集成）。 |
| **体量与运维** | **运行时极简**：一个二进制 + 一个工作区目录 + JSON 配置，适合小 VPS 或笔记本。 | 设计上组件更多（插件、UI 约定、平台能力）。 | 进程与配置面更大。 |
| **长上下文 / 召回** | **LCM 内置在同一二进制**（`lcm_*` 工具、SQLite DAG），该路径无需单独装插件。 | 能力强；LCM 类能力常以**插件**形式安装与接线。 | **记忆 + 提示 + skills** 组合强，偏持续学习场景。 |
| **通道与工具** | **多通道内置**；MCP 通过 `tools.mcpServers`；文件/Shell/网/浏览器/cron/spawn。 | 通过 OpenClaw 生态与 MCP 扩展。 | 工具与通道覆盖面很广；多通道故事强。 |
| **模型** | OpenAI 兼容 Base URL + 可选 Codex/Copilot 辅助。 | 依赖更宽的技术栈，灵活度高。 | OpenRouter / 门户 / 自定义端点等。 |

**更适合选 dipper-bot 时** — 需要**小体量、可审计的 Go 代码**、**工作区优先**的个人 Agent、希望 **LCM + MCP + cron 一体化**而不搭大型 Node 平台，且接受用 **JSON 配置**和**精简 CLI** 驱动行为。

## 致谢

| 项目 | 组织 / 上游 | 链接 |
|------|----------------|------|
| **OpenClaw** | OpenClaw | [github.com/openclaw/openclaw](https://github.com/openclaw/openclaw) |
| **nanobot** | HKUDS | [github.com/HKUDS/nanobot](https://github.com/HKUDS/nanobot) |
| **Hermes Agent** | Nous Research | [github.com/NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) |

## 许可证

MIT License。见本仓库中的 [LICENSE](./LICENSE)。
