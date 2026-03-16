## acgo 对齐 pi-mono 的改进路线图

本文档按优先级列出了 acgo 后续需要完成的工作，用于逐步向 `badlogic/pi-mono` 的能力靠拢。可以直接按 checklist 拆分 PR / 任务。

---

## P0：核心能力（优先完成）

### P0.1 LLM 抽象与 Provider 支持

- [x] **事件模型补全（`pkg/llm`）**（部分完成）
  - [x] 在 `llm.Event` 中已有：reasoning / thinking 事件（`EventThinkingStart/Delta/End`）、usage（`Usage`）、stop reason（`StopReason`）。
  - [x] Agent 层处理 `EventThinking*`，累积到 `AgentMessage.Thinking`，并通过 `EventMessageUpdate` 透传 `LlmEvent`。
  - [x] TUI 订阅 `EventMessageUpdate`，展示流式 `TextDelta` 与 `ThinkingDelta`，并在完成时写入 history（先 Thinking 后 Assistant）。
  - [x] DeepSeek 思考模式：配置层面通过 **model.reasoning**（如 `deepseek-reasoner` 为 ReasoningHigh）或 **agent.ThinkingLevel** 控制；请求中对 DeepSeek 自动添加 `thinking: {"type": "enabled"}`，并解析 `reasoning_content`，在模拟流中发出 `EventThinking*`；多轮/工具调用时通过 `reasoning_content` 回传上一轮思考内容以符合 [DeepSeek 文档](https://api-docs.deepseek.com/zh-cn/guides/thinking_mode)。
  - [ ] 各 Provider 若支持**真实流式** reasoning（如 DeepSeek 流式 SSE），需在 Stream 实现中解析并发出 `EventThinking*`；当前为 Complete 后模拟流。

- [x] **ToolCall 参数解析增强（`pkg/llm` + `pkg/agent`）**
  - [x] 明确 `ToolCall.Arguments` 的约定（始终为 JSON 字符串或 `json.RawMessage`）。
  - [x] 在流式 `toolcall_delta` 阶段尝试部分解析参数，保证始终有可用的 `arguments` 结构（对标 `pi-ai` 的增量解析策略）。
  - [x] 在 `agent.executePendingTools` 中统一处理双重 JSON 编码等兼容问题，并写单元测试覆盖常见模型输出格式。


### P0.2 Agent 运行时增强（`pkg/agent`）

- [x] **AgentState 操作 API 丰富化**
  - [x] 增加 `ReplaceMessages([]AgentMessage)`，用于批量替换历史消息。
  - [x] 增加 `SetError(error)` / `ClearError()`，代替直接修改 `state.Error`。
  - [x] 视需要增加 `WaitForIdle(ctx)`，用于在 UI / 测试中等待当前流式调用结束。

- [x] **Steering / Follow-up 队列机制**
  - [x] 在 `AgentState` 中新增字段：
    - [x] `SteeringQueue []AgentMessage`
    - [x] `FollowUpQueue []AgentMessage`
  - [x] 约定：
    - [x] Agent 忙时，可往队列插入新的 user/steering 消息。
    - [x] 当前 turn 结束后，优先消费 steering，再消费 follow-up。
  - [x] 在 `Agent.Prompt` 主循环中接入上述队列逻辑，确保不会与现有调用产生死锁或竞态。

- [x] **Context 裁剪（`TransformContext` 的实际实现）**
  - [x] 在 `defaultTransformContext` 中实现基础的消息裁剪策略，例如：
    - [x] 限制总 token 数（粗略估算）或轮次数（只保留最近 N 轮）。
    - [x] 始终保留 system 消息和最近若干条 toolResult。
  - [x] 为未来更智能的裁剪（摘要旧消息）预留扩展点。

- [x] **错误模型统一**
  - [x] 约定错误分类：LLM 调用错误 / 工具执行错误 / 上下文错误 / 取消（abort）。
  - [x] 在 `Agent.Prompt` 的事件流中统一记录错误类型，并在 `EventAgentEnd` / `EventTurnEnd` 中暴露。
  - [x] 在 TUI / CLI 层根据错误类型展示更友好的提示。

### P0.3 基础工具集补全（`pkg/tools`）

- [x] **现有工具对齐**
  - [x] 检查 `read` / `write` / `bash` 的 JSON schema 与错误处理：
    - [x] 确认参数命名、描述清晰，适合 LLM prompt。
    - [x] 统一：参数错误返回 Go error，业务失败时设置 `ToolResult.IsError = true` 并提供可读 `Content`。

- [x] **新增 `edit` 工具**
  - [x] 设计 JSON schema（建议）：
    - [x] `path`: 目标文件
    - [x] `instructions` 或 `diff`: 说明改动或直接提供 patch
  - [x] 实现一个“文本级”增量编辑器（可先从简单的“用提示生成新内容替换文件”开始，后续再做更精细的 diff）。
  - [x] 为 `edit` 写集成测试，确保在多轮调用中行为可预测。

- [x] **新增项目浏览/搜索工具**
  - [x] `grep`：
    - [x] 参数：`pattern`、`path`（可选，默认当前仓库根目录）、`max_results` 等。
    - [x] 返回匹配的文件路径 + 行号 + 片段。
  - [x] `find` / `ls`：
    - [x] 输入：目录路径 + 可选 glob 过滤。
    - [x] 输出：文件/目录列表，便于 LLM 了解代码结构。

- [x] **在 TUI 的默认 Agent 中注入所有基础工具**
  - [x] 在 `tui.NewModel()` 里扩展 `builtinTools`，把新工具一起注册到 Agent。

### P0.4 Session 接入主流程（`pkg/session` + CLI/TUI）

- [x] **Session 存储路径与命名规范**
  - [x] 在配置中定义全局 session 根目录（如 `~/.acgo/sessions`，支持覆盖）。
  - [x] 约定 session 文件命名方式（例如 `YYYYMMDD-HHMMSS-<random>.jsonl`）。

- [x] **在 `acgo chat` 中创建/加载 Session**
  - [x] 启动 TUI 时：
    - [x] 若未指定 `--session`，创建一个新的 session 文件。
    - [x] 若指定 `--session`，则加载历史消息（后续可映射为 AgentMessage）。
  - [x] 在每次 user / assistant / tool 消息产生时，调用 `session.AppendMessage` 追加到 JSONL。

- [x] **基础 session 管理命令雏形**
  - [x] 预留 CLI 入口（例如 `acgo session list` / `acgo session show`，可以后续再细化）。
  - [x] 在 TUI 中至少展示当前 session 路径，便于用户排查问题。

---

## P1：体验与开发者工作流（重要，但可推迟）

### P1.1 TUI 交互体验增强（`pkg/tui`）

- [ ] 支持多行输入与快捷键：
  - [ ] 区分 Enter 发送与 Shift+Enter / Ctrl+Enter 换行。
- [ ] 中断当前 LLM 流：
  - [ ] 将某个快捷键（如 Ctrl+C）映射为调用 `Agent.Abort()`，而非直接退出程序。
  - [ ] 结合 steering 队列，在中断后根据需要把未发送内容回填编辑器。
- [ ] 在 UI 中展示简单状态行：
  - [ ] 当前模型 / Provider
  - [ ] 是否正在 streaming
  - [ ] 最近一次错误的简短提示（若有）

### P1.2 `/` 命令系统设计

- [ ] 在输入层解析 `/command args`，避免直接发给 LLM。
- [ ] 首批内置命令：
  - [ ] `/model`：列出并切换当前模型。
  - [ ] `/session`：显示当前 session 信息（后续可扩展为 list / switch）。
  - [ ] `/reset`：重置 AgentState（清空消息与错误）。
  - [ ] `/settings`：打印配置文件位置或打开方式提示。
  - [ ] `/quit`：退出程序。
- [ ] 设计命令注册机制：
  - [ ] 使用一个简单的 registry（map[name]CommandHandler），便于日后在代码或扩展中注册新命令。

### P1.3 配置与上下文文件体系

- [ ] 定义上下文文件查找规则：
  - [ ] 全局：`~/.acgo/AGENTS.md` / `SYSTEM.md` / `APPEND_SYSTEM.md`
  - [ ] 项目级：从当前工作目录向上查找 `AGENTS.md` / `SYSTEM.md` / `APPEND_SYSTEM.md`
  - [ ] 当前目录：优先级最高。
- [ ] 在启动 Agent 时：
  - [ ] 读取上述文件，合成最终 `SystemPrompt`。
  - [ ] 在日志中记录加载到的文件路径，便于调试。
- [ ] 提供一个简单命令（或按键）展示当前生效的 system prompt 摘要。

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

