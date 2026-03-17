## acgo 对齐 pi-mono 的改进路线图

本文档按优先级列出了 acgo 后续需要完成的工作，用于逐步向 `badlogic/pi-mono` 的能力靠拢。可以直接按 checklist 拆分 PR / 任务。

---

## P0：核心能力（优先完成）

---

## P1：体验与开发者工作流（重要，但可推迟）

### P1.1 TUI 交互体验增强（`pkg/tui`）

- [ ] 支持多行输入与快捷键：
  - [ ] 区分 Enter 发送与 Shift+Enter / Ctrl+Enter 换行（pi-mono 行为）。
- [ ] 中断与状态行：基础能力见 **P0.1**、**P0.5**；此处可扩展为更丰富的状态信息或快捷键（如 Alt+Up 从队列取回）。
- [ ] （可选）`@` 文件引用、Tab 路径补全、`!cmd`/`!!cmd` 等 pi-mono 编辑器特性。

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

