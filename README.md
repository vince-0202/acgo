## ACGO

> **参考项目 `badlogic/pi-mono`。**

用于构建 AI Agent、管理 LLM 部署的 Go CLI / TUI 工具。基于`badlogic/pi-mono`项目思想进行构建并加强。

### 功能概览

- **TUI 对话体验**：在终端中与 Agent 进行多轮对话，支持上下文管理与工具调用（计划逐步对齐 `pi-mono` 的 `packages/tui` 交互体验）。
- **LLM Provider 抽象**：通过统一接口接入多家模型 Provider（参考 `pi-mono` 的 `packages/ai` 设计，逐步演进中）。
- **Agent 运行时**：封装会话状态与工具调用（参考 `pi-mono` 的 `packages/agent` 能力，逐步补齐）。

### 快速开始

以下步骤介绍如何使用acgo创建一个agent并与agent进行一轮交互。代码在 `example/prompt_one_with_settings.go`
#### 1.创建配置文件信息
在 `${HOME}/.acgo/` 目录下创建 `settings.yaml`文件。
```yaml
agent:
 default_provider: deepseek
 default_model: deepseek-reasoner
 providers:
    - provider: deepseek
      api_key: xxxx
log:
 level: debug
```

#### 2.加载配置文件信息
```go
//load config from ${HOME}/.acgo/settings.yaml
settings, err := bootstrap.LoadAndRuntimeInit(
	config.WithDirectName(".acgo"),
	config.WithName("settings"))
```
#### 3.构建agent
```go
//build agent with settings and other options
ag := bootstrap.BuildAgent(
	settings,
	bootstrap.WithId("example-bootstrap-agent"),
	bootstrap.WithDefaultTools())
```

#### 4.注册监听函数
```go
unsub := ag.Subscribe(func(e agent.Event) {
		switch e.Type {
		case agent.EventMessageEnd:
			fmt.Printf("%s: %s\n", e.Message.Role, e.Message.Content)
		}
	})
defer unsub()
```
#### 5.发送消息并由注册函数打印结果
```go
userText := "你好，请用一句话介绍你自己。"
if err := ag.Prompt(context.Background(), userText); err != nil {
	return fmt.Errorf("prompt failed: %w", err)
}
```

### 贡献

欢迎通过 Issue / PR 参与，共同将 `acgo` 打造成 Go 生态下的 `pi-mono` 风格 Agent 工具。

在提交前请先阅读：

- `docs/roadmap/global.md`：整体规划与优先级
- （预留）`CONTRIBUTING.md` / `AGENTS.md`：贡献与 Agent 协作规范，如后续添加


#### 开发

```bash
go version           # 确认 Go 版本
go test ./...        # 运行测试（如有）
go run ./cmd/acgo    # 从源码运行 acgo
```

#### 构建

```bash
make build           # 构建
make run        # 基于构建结果运行chat命令
```


### License

MIT

