## acgo 对齐 pi-mono 的改进路线图

本文档按优先级列出了 acgo 后续需要完成的工作，用于逐步向 `badlogic/pi-mono` 的能力靠拢。可以直接按 checklist 拆分 PR / 任务。

---

## P0：核心能力（优先完成）

### P0.1 中断与退出语义（TUI ↔ Agent）

- [x] **Ctrl+C / Escape 行为**：
  - [x] Agent 正在 streaming 时：调用 `Agent.Abort()` 中断当前轮次，不退出程序；已生成内容保留到 history。
  - [x] 仅当未在 streaming 时 Ctrl+C / Esc 退出程序；`/quit` 始终退出。
- [x] 在 TUI 的 `Update(tea.KeyMsg)` 中根据 `agent.State().IsStreaming` 区分「中断」与「退出」。

### P0.2 `/` 命令系统（输入层解析）

- [x] **输入层拦截**：以 `/` 开头的输入不发给 LLM，解析为命令（可带参数）。
- [x] **首批内置命令**（与 pi-mono 对齐）：
  - [x] `/model`：列出当前可用模型；`/model <id>` 切换模型。
  - [x] `/session`：显示当前 session 路径。
  - [x] `/reset`：调用 `Agent.Reset()`，清空消息与错误状态及 TUI history。
  - [x] `/settings`：打印配置文件路径（`~/.acgo/settings.yaml`）。
  - [x] `/quit` 或 `/q`：退出程序。
- [x] 命令执行结果在 TUI history 中以 `> ...` 展示。

### P0.3 Context 文件与 System Prompt 合成

- [x] **多层级上下文文件**（`pkg/contextfile`）：
  - [x] 全局：`~/.acgo/AGENTS.md`、`SYSTEM.md`、`APPEND_SYSTEM.md`（若存在则读取）。
  - [x] 项目级：从当前工作目录向上查找目录中的同名文件，当前目录优先级最高。
  - [x] 约定：`SYSTEM.md` 替换默认文案，`AGENTS.md` / `APPEND_SYSTEM.md` 追加。
- [x] **启动时合成**：在 `NewModel` 中调用 `contextfile.Load(workDir)`，将结果注入 `InitialState.SystemPrompt`。
- [x] 在 debug 日志中记录加载到的文件路径。

### P0.4 TUI 侧 Steering / Follow-up 对接

- [x] **Agent 忙时的输入行为**（与 `pkg/agent` 已有队列对接）：
  - [x] 当 `Agent.IsStreaming == true` 时，Enter 将当前输入作为 **steering** 调用 `Agent.EnqueueSteering()`，清空输入框。
  - [x] Alt+Enter 将当前输入作为 **follow-up** 调用 `Agent.EnqueueFollowUp()`，清空输入框。
- [x] Escape 在忙时调用 `Abort()`（见 P0.1）；空闲时退出。
- [x] TUI 通过 `agent.State().IsStreaming` 切换 Enter/Alt+Enter 语义。

### P0.5 简单状态行（TUI）

- [x] 在 TUI 顶部增加一行状态栏（`statusLine()`），展示：
  - [x] 当前模型（`provider/id`）、streaming / idle、最近一次错误（若有）。
  - [x] 与 Session 路径合并为一行显示。

---

## P1：体验与开发者工作流（重要，但可推迟）

### P1.1 TUI 交互体验增强（`pkg/tui`）

- [ ] 支持多行输入与快捷键：
  - [ ] 区分 Enter 发送与 Shift+Enter / Ctrl+Enter 换行（pi-mono 行为）。
- [ ] 中断与状态行：基础能力见 **P0.1**、**P0.5**；此处可扩展为更丰富的状态信息或快捷键（如 Alt+Up 从队列取回）。
- [ ] （可选）`@` 文件引用、Tab 路径补全、`!cmd`/`!!cmd` 等 pi-mono 编辑器特性。

### P1.2 `/` 命令系统扩展

- [ ] 基础命令实现见 **P0.2**。
- [ ] 设计命令注册机制：
  - [ ] 使用一个简单的 registry（map[name]CommandHandler），便于日后在代码或扩展中注册新命令。
- [ ] 扩展更多命令（如 `/new` 新会话、`/tree` 会话树、`/name` 等），与 pi-mono 对齐。

### P1.3 配置与上下文文件体系（扩展）

- [ ] 基础上下文文件加载与合成见 **P0.3**。
- [ ] 提供一个简单命令（或按键）展示当前生效的 system prompt 摘要。
- [ ] 支持热重载或 `/reload` 重新读取 context 文件（可选）。

### P1.4 模型与 Provider 选择 UX

- [ ] 基于 `llm.ListModels()` 和配置，提供人类可读的模型列表。
- [ ] 在 `/model` 命令中支持：
  - [ ] 按 Provider 过滤。
  - [ ] 简单的别名（如 `default` / `fast` / `long`）。
- [ ] 在日志中记录每次模型切换（旧值 → 新值）。

---

## P2：生态扩展与高级特性（中长期）

### P2.1 更多 Provider 与多模态支持

- [ ] 按优先级逐步增加新的 Provider（如 Anthropic、Gemini 等），保持与 `llm.Provider` 接口兼容。
- [ ] 扩展 `llm.Message.Content` 结构，以支持 image 等多模态输入：
  - [ ] 设计 content block 类型（text / image / …）。
  - [ ] 在 Provider 层做必要的适配与限制检查。

### P2.2 Web UI 与 RPC 接口

- [ ] 定义一个简单的 JSONL / HTTP RPC 协议，暴露 Agent 能力：
  - [ ] 发送 user 消息、接收流式 delta、执行工具等。
- [ ] 在此协议之上，可以开发：
  - [ ] 最小 Web 聊天 UI。
  - [ ] 与其他编辑器 / 工具的集成。

### P2.3 扩展 / 插件机制

- [ ] 设计扩展点接口：
  - [ ] 允许注册额外工具。
  - [ ] 允许注册额外 `/` 命令。
- [ ] 选择一种简单协议（如基于 stdin/stdout 的 JSON-RPC）与外部进程交互，避免 Go 插件在多平台上的部署问题。

### P2.4 性能与观测

- [ ] 为关键路径增加简单 metrics / 日志：
  - [ ] 每次调用的 token 估计。
  - [ ] 工具执行耗时。
  - [ ] 上下文长度（消息数或估算 token 数）。
- [ ] 根据观测数据，迭代 `TransformContext` 的裁剪策略（例如更积极地摘要老消息）。

