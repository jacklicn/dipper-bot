# 构建与开发

**语言**：本文为中文版；英文版见 [BUILD.md](./BUILD.md)。

从源码编译 **dipper-bot**、运行测试、了解仓库结构。日常命令与交互式 CLI 见 [MANUAL_CN.md](./MANUAL_CN.md)。`config.json` 全部字段见 [CONFIG_CN.md](./CONFIG_CN.md)。

## 环境要求

- **Go 1.26**（若使用更低版本需自行调整 `go.mod`）

## 编译

**macOS / Linux：**

```bash
cd dipper-bot
go build -o dipper-bot .
# 可选：make
```

**Windows（PowerShell）：**

```powershell
cd dipper-bot
go build -o dipper-bot.exe .
```

首次使用：

```bash
./dipper-bot onboard          # 生成 ~/.dipper-bot/config.json 与工作区
# 在 workspace/config.json 中填写 providers.custom.apiKey
./dipper-bot agent -m "Hello!"
```

Windows 使用 `.\dipper-bot.exe`。双击 `.exe` 会进入交互式 REPL（UTF-8；若 emoji 显示异常请使用 [Windows Terminal](https://learn.microsoft.com/zh-cn/windows/terminal/install)）。

## 测试

```bash
go test ./...
```

## 仓库目录

| 路径 | 作用 |
|------|------|
| `main.go` | CLI 入口 |
| `agent/` | 上下文构建、循环、记忆、技能、自适应控制器 |
| `tools/` | 文件、exec、message、cron、spawn、web、浏览器（CDP）、压缩器、LCM 工具、MCP |
| `lcm/` | 无损上下文（SQLite、DAG 摘要） |
| `bus/` | 入站/出站消息总线 |
| `config/` | 配置结构、加载、迁移 |
| `gateway/` | HTTP 服务（`POST /message` 等） |
| `web/` | `agent --web` 网页对话 |
| `cron/` | 定时任务（`workspace/cron/jobs.json`） |
| `session/` | 会话管理（JSONL） |
| `channels/` | Telegram、WhatsApp、Discord、飞书、钉钉、Slack、邮件、QQ、企微、Webhook |
| `bridge/` | WhatsApp 桥接（Node.js / Baileys） |
| `deploy/` | Docker Compose、systemd、Windows 服务示例 |
| `heartbeat/` | 周期性 `HEARTBEAT.md` 检查 |
| `providers/` | OpenAI 兼容 HTTP 客户端 |

## 部署

- **Docker**：`docker compose up -d dipper-bot-gateway`（见 `deploy/`）
- **Linux（systemd）** / **Windows（NSSM、sc.exe）**：见 [deploy/README.md](../deploy/README.md)

## 相关文档

- [MANUAL_CN.md](./MANUAL_CN.md) — 安装、命令、通道使用（中文）
- [CONFIG_CN.md](./CONFIG_CN.md) — 完整配置说明（中文）
- [DESIGN_CN.md](./DESIGN_CN.md) — 架构说明（中文）
- [LCM_CN.md](./LCM_CN.md) — 无损上下文行为（中文）
