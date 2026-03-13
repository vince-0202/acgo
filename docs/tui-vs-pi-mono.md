# TUI 与 pi-mono 源码对照检查

对照 `pi-mono` 的 **pi-tui** 与 **pi-coding-agent** 交互/布局设计，对当前 Go 版 `pkg/tui` 的差异说明。

---

## 1. 布局与结构

| 能力 | pi-mono 原版 | 当前 Go TUI | 匹配 |
|------|--------------|-------------|------|
| 上方消息区 | 可滚动消息列表，每条带角色/内容/Markdown/代码高亮 | 固定高度 10 行的纯文本 `history`（"You: xxx" / "Assistant: xxx"） | 否 |
| 下方输入区 | 多行 **Editor**（路径补全、@ 引用、多行） | `textarea`，无补全、无 @ | 部分（多行有） |
| 状态栏 | 当前模型、Provider、token 消耗等 | 无 | 否 |
| 差分渲染 | pi-tui 只重绘变化行 + CSI 2026 防闪烁 | bubbletea 自行刷新，未做行级差分 | 实现不同 |

---

## 2. 编辑器与输入

| 能力 | pi-mono 原版 | 当前 Go TUI | 匹配 |
|------|--------------|-------------|------|
| 多行输入 | Shift+Enter / Ctrl+Enter 换行，Enter 发送 | textarea 支持多行，但 Enter 直接发送（未区分 Shift+Enter） | 否 |
| `@` 文件引用 | 输入 `@` 触发文件模糊搜索，插入路径 | 未实现 | 否 |
| Tab 路径补全 | 支持 | 未实现 | 否 |
| `!cmd` / `!!cmd` | `!cmd` 执行 bash 并把输出发给 LLM；`!!cmd` 只执行不发送 | 未解析 | 否 |
| 粘贴图片 | 支持图片消息 | 未实现 | 否 |

---

## 3. 流式输出与事件

| 能力 | pi-mono 原版 | 当前 Go TUI | 匹配 |
|------|--------------|-------------|------|
| Assistant 流式 | 订阅 Agent `message_update`，按 `text_delta` 逐字更新 UI | **非流式**：`askAgent` 等 `Prompt` 完全结束后才把整段 `last.Content` 写入 history | 否 |
| 工具执行展示 | `tool_execution_start/update/end` 在侧边或 overlay 显示 | 未订阅，工具执行过程不可见 | 否 |
| 中断 | Abort 后事件流结束，UI 显示已生成内容 | Agent 有 `Abort()`，TUI 未暴露（如 Ctrl+C 只退程序） | 部分 |

---

## 4. 命令系统

| 能力 | pi-mono 原版 | 当前 Go TUI | 匹配 |
|------|--------------|-------------|------|
| `/` 命令 | `/model` `/settings` `/session` `/new` `/tree` `/quit` 等 | 未解析，输入 `/xxx` 会当普通消息发给 LLM | 否 |
| 扩展命令 | 可注册自定义 `/command` | 无扩展机制 | 否 |

---

## 5. Steering / Follow-up 队列

| 能力 | pi-mono 原版 | 当前 Go TUI | 匹配 |
|------|--------------|-------------|------|
| Enter（Agent 忙时） | 编辑器内容作为 **steering** 入队，打断当前并插入 | 未实现，无队列 | 否 |
| Alt+Enter | 作为 **follow-up** 入队，当前任务结束后再执行 | 未实现 | 否 |
| Escape | 中断并把队列消息放回编辑器 | 未实现 | 否 |
| Alt+Up | 从队列取回一条到编辑器 | 未实现 | 否 |

---

## 6. Agent 与工具

| 能力 | pi-mono 原版 | 当前 Go TUI | 匹配 |
|------|--------------|-------------|------|
| 内置工具 | read / write / edit / bash / grep / find / ls 默认注入 Agent | **未注入**：`NewModel()` 里 `AgentState.Tools` 为空，聊天时无法调工具 | 否 |
| 模型选择 | 支持 default_provider + default_model，/model 切换 | 仅用 `settings.DefaultModelID`，未用 `DefaultProvider` | 部分 |

---

## 7. 配置与 Context 文件

| 能力 | pi-mono 原版 | 当前 Go TUI | 匹配 |
|------|--------------|-------------|------|
| AGENTS.md / SYSTEM.md | 多层级加载，拼成 system prompt | 未加载，写死 "You are a helpful coding assistant." | 否 |
| Session 持久化 | JSONL 树存 ~/.pi/agent/sessions | 未集成，每次启动全新对话 | 否 |

---

## 总结

- **与源码一致或接近的**：上方历史 + 下方输入的大致布局、多行输入（textarea）、Enter 发送、Esc/Ctrl+C 退出。
- **与源码不一致或缺失的**：
  1. **流式**：应订阅 Agent 的 `message_update`，按事件逐字更新 assistant 内容，而不是等整段再显示。
  2. **工具**：应在 `NewModel()` 里给 Agent 设置 `SetTools(read, write, bash, ...)`，并传入 `llm.Options.Tools`。
  3. **默认模型**：应支持 `DefaultProvider`，再按 provider 取 model 或 `DefaultModelID`。
  4. **/ 命令**：至少解析 `/model`、`/settings`、`/quit` 等，不把以 `/` 开头的当普通消息。
  5. **Steering/Follow-up**：Agent 忙时 Enter/Alt+Enter 入队，Esc 中断并回填编辑器。
  6. **@、!cmd、状态栏、Session、Context 文件**：为后续增强项。

建议优先在代码中补齐：**流式显示**、**为 Agent 注入内置工具**、**使用 DefaultProvider 选模型**，这样 TUI 与 pi-mono 的交互与行为会更一致。
