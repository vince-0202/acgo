## acgo 对齐 pi-mono 的改进路线图

本文档按优先级列出了 acgo 后续需要完成的工作，用于逐步向 `badlogic/pi-mono`（`pi-ai` / `pi-agent-core` / `pi-coding-agent` / `pi-tui` / 扩展生态）靠拢。每条 checklist 都应当能直接拆成 PR / Issue。

对齐口径（以 pi-mono 的分层为参照）：
- **LLM 统一层（对标 `pi-ai`）**：多 Provider、统一事件流（text/thinking/toolcall/usage/stopReason/error）、模型元信息、兼容层、多模态。
- **Agent 运行时（对标 `pi-agent-core`）**：turn/消息/工具执行事件、队列（steering/follow-up）、上下文变换与裁剪、abort/错误模型。
- **终端产品（对标 `pi-coding-agent` + `pi-tui`）**：交互式编辑器、`/` 命令系统、会话树、配置/上下文文件、默认工具集、可扩展能力（skills/prompts/themes/extensions）。

---

## P0：核心能力（优先完成）

### 现状（已完成项汇总）

以下条目已在阶段性记录中完成（作为 P0 基础能力的前置）：
- `docs/roadmap/finishat20260314.md`
- `docs/roadmap/finishat20260316.md`
- `docs/roadmap/finishat20260317.md`

> 这些完成项会在下面的 P0/P1 checklist 中以 `[x]` 体现；新增能力以 `[ ]` 继续推进。

### P0.1 LLM 抽象与事件流（对标 `pi-ai`）

- [x] **事件模型基础**：text/thinking/toolcall/usage/stopReason（`pkg/llm`）并贯穿到 Agent/TUI 展示。
- [x] **ToolCall 参数增量解析**：流式阶段可部分解析，保证 arguments 始终可用（对标 `pi-ai` 的增量 JSON 解析策略）。
- [ ] **Provider 能力矩阵与模型元信息**（对标 `Model` 抽象）：
  - [ ] 为每个模型补齐并可查询：`contextWindow`、`maxTokens`、是否支持 `image`、是否支持 `reasoning`。
  - [ ] 统一 `llm.ListModels()` 输出：支持按 provider / capability 过滤。
- [ ] **跨 Provider 上下文迁移（handoff）规则**：
  - [ ] 定义不同 provider 间 assistant/thinking 的降级策略（对标 pi-mono：thinking block 转文本标签）。
  - [ ] 明确 toolResult 的兼容字段（是否需要 name、是否需要额外 assistant 消息等）。
- [ ] **“兼容层（compat）”设计**：为 OpenAI-compatible/不同实现差异提供开关（字段名、developer role、reasoning_effort、toolResult 约束等）。
- [ ] **从“Complete 后模拟流”逐步升级为真实流式**：
  - [ ] 优先把最常用 provider 的 streaming 做成真实 SSE/流式解析（包括 reasoning）。
  - [ ] 统一 abort 语义：中断时返回已生成部分 + usage（如有）并标记 stopReason。

### P0.2 Agent 运行时（对标 `pi-agent-core`）

- [x] **AgentState 操作 API**：ReplaceMessages/SetError/ClearError 等，避免外部直接改 state。
- [x] **steering / follow-up 队列**：忙时可打断/追加，turn 结束后按优先级消费。
- [x] **Context 裁剪（TransformContext）**：基础 token/轮次裁剪策略 + 扩展点。
- [x] **错误模型统一**：LLM/工具/上下文/取消（abort）分类，并在事件中暴露给 UI。
- [ ] **事件粒度与对齐**（便于未来 Web/RPC/录制回放）：
  - [ ] 明确并稳定：agent_start/end、turn_start/end、message_start/update/end、tool_execution_* 的事件字段与顺序。
  - [ ] 为 tool execution 增加可选的流式 onUpdate（长耗时工具实时输出）。
- [ ] **会话一致性约束**：
  - [ ] 约定消息 ID / parentId 规则（为“会话树 / fork / 回溯”打底）。
  - [ ] 约定在 abort/错误时，history 如何落盘与如何恢复。

### P0.3 默认工具集（对标 `pi-coding-agent` 内置工具）

- [x] **read / write / bash**：schema/错误处理梳理并注入默认 Agent。
- [x] **edit 工具**：增量编辑能力 + 集成测试。
- [x] **grep / find / ls**：项目浏览与检索工具，返回行号与片段，便于模型定位代码。
- [ ] **工具参数校验标准化**：
  - [ ] 为所有工具统一 JSON schema 校验与错误回传格式（对标 `pi-ai` 的 AJV 校验思路）。
  - [ ] 明确“工具失败抛错 vs 返回 isError toolResult”的约定与最佳实践。

### P0.4 Session 持久化（对标 `pi` 的 JSONL 会话树）

- [x] **Session 文件落盘**：在 `acgo chat` 启动时创建/加载 session，消息 JSONL 追加写入。
- [x] **基础 session 管理入口预留**：CLI 入口雏形 + TUI 展示当前 session 路径。
- [ ] **会话树能力（最小可用）**：
  - [ ] `/tree`：展示当前 session 的分支结构（折叠/过滤可后置）。
  - [ ] `/fork`：从当前节点创建新分支继续对话（新 session 文件或同文件分支，二选一并文档化）。
  - [ ] `/resume`：选择历史 session 继续。
- [ ] **`/compact` 上下文压缩**：手动触发总结/压缩（先做规则化压缩，再逐步引入摘要）。

### P0.5 TUI/命令系统（对标 `pi-tui` + `pi` 的基础交互）

- [x] **中断与退出语义**：忙时 Ctrl+C/Esc 中断 streaming，空闲时退出；`/quit` 始终退出。
- [x] **`/` 命令系统**：输入层拦截并执行命令（`/model` `/session` `/reset` `/settings` `/quit` 等）。
- [x] **Context 文件与 System Prompt 合成**：全局 + 项目级多层级加载，支持 `/reload`（可选）。
- [x] **Steering / Follow-up 键位对接**：Enter/Alt+Enter 在忙时入队。
- [x] **简单状态行**：展示模型、streaming/idle、最近错误、session 路径。

## P1：体验与开发者工作流（重要，但可推迟）

### P1.1 TUI 交互体验增强（`pkg/tui`）

- [ ] **多行输入与快捷键**（对标 pi-mono Editor 行为）：
  - [ ] 区分 Enter 发送与 Shift+Enter / Ctrl+Enter 换行。
  - [ ] Alt+Up：从 steering/follow-up 队列取回到编辑器（或提供等价交互）。
- [ ] **引用与补全**：
  - [ ] `@` 文件引用（模糊搜索 + 插入路径）。
  - [ ] Tab 路径补全（基于当前工作目录/仓库根目录）。
- [ ] **命令执行便捷输入**：
  - [ ] `!cmd`：执行 bash 并把输出作为消息发送给 Agent。
  - [ ] `!!cmd`：只执行不发送（用于准备环境/快速查看）。
- [ ] **渲染体验**：
  - [ ] Thinking 展示可折叠/可开关（避免刷屏）。
  - [ ] 工具执行输出与最终 toolResult 更清晰区分（流式 update vs end）。

### P1.4 模型与 Provider 选择 UX

- [ ] **人类可读模型列表**：基于 `llm.ListModels()` + 配置分组展示（provider → models）。
- [ ] **`/model` 扩展**：
  - [ ] 按 provider / capability 过滤。
  - [ ] 别名（`default` / `fast` / `long`）与项目级覆盖。
  - [ ] 记录每次切换：旧值 → 新值（并写入 session 事件或日志）。
- [ ] **“推荐模型”策略**：根据任务类型（长上下文/快速/推理/多模态）给出默认选择（先规则化，后可学习化）。

---

## P2：生态扩展与高级特性（中长期）

### P2.1 更多 Provider 与多模态支持

- [ ] 按优先级逐步增加新的 Provider（如 Anthropic、Gemini 等），保持与 `llm.Provider` 接口兼容。
- [ ] 扩展 `llm.Message.Content` 结构，以支持 image 等多模态输入：
  - [ ] 设计 content block 类型（text / image / …）。
  - [ ] 在 Provider 层做必要的适配与限制检查。
  - [ ] 在 TUI 侧定义降级策略（不支持 inline image 的终端如何展示）。

### P2.2 Web UI 与 RPC 接口

- [ ] 定义一个简单的 JSONL / HTTP RPC 协议，暴露 Agent 能力：
  - [ ] 发送 user 消息、接收流式 delta、执行工具等。
- [ ] 在此协议之上，可以开发：
  - [ ] 最小 Web 聊天 UI。
  - [ ] 与其他编辑器 / 工具的集成。

### P2.3 扩展 / 插件机制

- [ ] **扩展点接口**：
  - [ ] 允许注册额外工具（含 schema/权限/资源约束）。
  - [ ] 允许注册额外 `/` 命令（含帮助文档、参数解析）。
  - [ ] 允许监听事件（用于日志、权限控制、Git checkpoint、统计等）。
- [ ] **外部扩展进程协议**：
  - [ ] 基于 stdin/stdout 的 JSON-RPC（或等价）与外部进程交互，避免 Go plugin 的跨平台问题。
  - [ ] 设计扩展的生命周期与隔离（超时、取消、并发限制）。
- [ ] **资源分发形态**（对标 pi 的 skills/prompts/themes/packages）：
  - [ ] skills（工作流说明书）目录规范与加载规则。
  - [ ] prompts（模板）与 themes（主题）目录规范与热重载策略。
  - [ ] “包”概念：支持从 git/本地/registry 安装与更新（先最小实现）。

### P2.4 性能与观测

- [ ] 为关键路径增加简单 metrics / 日志：
  - [ ] 每次调用的 token 估计。
  - [ ] 工具执行耗时。
  - [ ] 上下文长度（消息数或估算 token 数）。
- [ ] 根据观测数据，迭代 `TransformContext` 的裁剪策略（例如更积极地摘要老消息）。

