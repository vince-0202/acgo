# ACGO 项目结构分析

## 项目概述

**ACGO** 是一个参考 `badlogic/pi-mono` 架构的 Go 语言实现，旨在提供 AI Agent 开发、LLM 部署管理的 CLI/TUI 工具。项目采用标准 Go 项目结构，分层清晰，模块化设计。

## 一、整体布局（标准 Go 项目结构）

```
.
├── cmd/                    # 命令行入口
├── pkg/                    # 核心库（可复用的 Go 包）
├── example/                # 使用示例
├── docs/                   # 文档（路线图等）
├── dist/                   # 构建输出目录（通常存放二进制）
├── .github/                # GitHub Actions 工作流
├── .gitignore
├── LICENSE
├── Makefile                # 构建、测试、运行脚本
├── go.mod / go.sum         # Go 模块定义与依赖
├── README.md               # 项目总览
└── pi‑mono‑analysis.md     # 对 pi‑mono 项目的详细分析（对齐依据）
```

## 二、核心目录详解

### 1. **`cmd/` – 命令行入口**
- `acgo/` – 主程序入口，提供多个子命令：
  - `main.go` – 根命令定义
  - `chat.go` – 聊天对话命令
  - `print.go` – 打印输出命令
  - `rag.go` – RAG 相关操作命令
  - `root.go` – 命令树根
  - `session.go` – 会话管理命令
- **作用**：将底层库包装成用户可直接执行的 CLI 工具，对应 `pi‑mono` 的 `pi` 命令行工具。

### 2. **`pkg/` – 核心库（按功能分层）**

| 包名 | 职责 | 对应 `pi‑mono` 模块 |
|------|------|---------------------|
| `agent/` | **Agent 运行时**：定义 `Agent` 类型，管理消息流、工具调用、事件订阅等 | `@mariozechner/pi‑agent‑core` |
| `bootstrap/` | **初始化辅助**：加载配置、构建 Agent、创建会话的便捷函数 | –（pi‑mono 中分散在 `coding‑agent` 初始化逻辑） |
| `config/` | **配置管理**：读取 `settings.yaml`，定义 Agent、Provider、RAG 等配置结构 | `~/.pi/agent/settings.json` 等 |
| `contextfile/` | **上下文文件**：加载 `AGENTS.md` 等上下文文件 | `coding‑agent` 的 Context Files 机制 |
| `keys/` | **API 密钥管理** | – |
| `llm/` | **LLM Provider 统一抽象**：为不同厂商（OpenAI、Anthropic、Gemini 等）提供统一接口 | `@mariozechner/pi‑ai` |
| `log/` | **日志记录** | – |
| `memory/` | **记忆存储**：管理 Agent 的长期记忆、上下文压缩等 | `pi‑agent‑core` 的 `AgentState` 部分 |
| `rag/` | **检索增强生成**：包含 Embedder、Vector DB（Qdrant）、Retriever 等 | –（pi‑mono 中通过扩展实现） |
| `runtime/` | **运行时环境** | – |
| `session/` | **会话管理**：持久化会话、分支、回溯 | `coding‑agent` 的 Session 管理 |
| `skills/` | **技能管理**：加载、执行符合 Agent Skills 标准的技能 | `coding‑agent` 的 Skills 扩展 |
| `tools/` | **内置工具实现**：`read`、`write`、`edit`、`bash`、`grep` 等 | `coding‑agent` 的默认工具集 |
| `tui/` | **终端用户界面**：基于 `bubbletea` 的 TUI 组件与交互 | `@mariozechner/pi‑tui` |
| `utils/` | **通用辅助函数** | – |

### 3. **`example/` – 示例代码**
- `prompt_one_with_settings.go` – 演示如何加载配置、构建 Agent、发送提示并接收回复。
- **作用**：供开发者快速上手，了解核心 API 的使用方式。

### 4. **`docs/` – 文档**
- `roadmap/` – 开发路线图：
  - `global.md` – 整体规划与优先级
  - `finishat20260314.md` 等 – 阶段性完成记录
- **作用**：记录项目演进方向，对齐 `pi‑mono` 的功能进度。

## 三、关键文件说明

| 文件 | 作用 |
|------|------|
| `pi‑mono‑analysis.md` | **重要**：对 `badlogic/pi‑mono` 的详细分层分析，是 `acgo` 实现的功能对标依据。 |
| `Makefile` | 提供 `make build`、`make run` 等快捷命令，简化开发流程。 |
| `go.mod` | 定义项目依赖，包括：<br>• `charmbracelet/bubbletea` – TUI 框架<br>• `spf13/cobra` – CLI 框架<br>• `spf13/viper` – 配置解析<br>• `openai/openai‑go/v3` – OpenAI SDK<br>• `qdrant/go‑client` – 向量数据库客户端<br>• 其他 LLM Provider 的 SDK |

## 四、与 `pi‑mono` 的架构对标

| 层次 | `pi‑mono`（TypeScript） | `acgo`（Go） | 状态 |
|------|-------------------------|--------------|------|
| **统一 LLM 层** | `@mariozechner/pi‑ai` | `pkg/llm/`（各 Provider 子目录） | 逐步实现中 |
| **Agent 运行时** | `@mariozechner/pi‑agent‑core` | `pkg/agent/` | 核心消息流、工具调用已具备 |
| **终端 Coding Agent** | `@mariozechner/pi‑coding‑agent` | `cmd/acgo/` | 提供 chat、session 等命令 |
| **终端 UI 框架** | `@mariozechner/pi‑tui` | `pkg/tui/`（基于 bubbletea） | 基础 TUI 组件 |
| **扩展与技能** | Skills、Extensions、Prompt Templates | `pkg/skills/`、`pkg/tools/` | 技能加载、内置工具已实现 |
| **配置与会话** | `~/.pi/agent/` 下的配置与会话文件 | `pkg/config/`、`pkg/session/`、`pkg/contextfile/` | 支持 YAML 配置、会话持久化 |

## 五、开发与构建

1. **运行**：`go run ./cmd/acgo` 或 `make run`
2. **构建**：`make build`（输出到 `dist/`）
3. **测试**：`go test ./...`（目前测试文件较少）
4. **配置**：在 `~/.acgo/settings.yaml` 中配置 Provider、模型等。

## 总结

**ACGO** 是一个**分层清晰、模块化**的 Go 项目，旨在将 `pi‑mono` 的现代 AI Agent 开发生态移植到 Go 语言环境。其结构严格对应 `pi‑mono` 的核心分层（LLM → Agent → CLI/TUI → 扩展），同时利用 Go 的静态编译、并发模型等特性，为 Go 开发者提供一套完整的 AI Agent 开发工具链。

目前项目处于**积极演进**阶段，各项功能正在逐步对齐 `pi‑mono`（详见 `docs/roadmap/`），欢迎通过 Issue/PR 参与贡献。