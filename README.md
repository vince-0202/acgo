## acgo

> **参考项目 `badlogic/pi-mono`。**

用于构建 AI Agent、管理 LLM 部署的 Go CLI / TUI 工具，目前正按 `docs/roadmap/global.md` 中的路线逐步对齐 `pi-mono` 的能力。

### 功能概览（进行中）

- **TUI 对话体验**：在终端中与 Agent 进行多轮对话，支持上下文管理与工具调用（计划逐步对齐 `pi-mono` 的 `packages/tui` 交互体验）。
- **LLM Provider 抽象**：通过统一接口接入多家模型 Provider（参考 `pi-mono` 的 `packages/ai` 设计，逐步演进中）。
- **Agent 运行时**：封装会话状态与工具调用（参考 `pi-mono` 的 `packages/agent` 能力，逐步补齐）。

> 详细的对齐与改进计划见 `docs/roadmap/global.md`。

### 开发

```bash
go version           # 确认 Go 版本
go test ./...        # 运行测试（如有）
go run ./cmd/acgo    # 从源码运行 acgo
```

后续可根据路线图补充：

- 构建 / 发布脚本
- 对应的 `make` / `task` 命令

### 贡献

欢迎通过 Issue / PR 参与，共同将 `acgo` 打造成 Go 生态下的 `pi-mono` 风格 Agent 工具。

在提交前请先阅读：

- `docs/roadmap/global.md`：整体规划与优先级
- （预留）`CONTRIBUTING.md` / `AGENTS.md`：贡献与 Agent 协作规范，如后续添加

### License

MIT

