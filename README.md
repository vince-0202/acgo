# ACGO

[中文 README](docs/README.zh-CN.md)

> Inspired by `badlogic/pi-mono`.

ACGO is a Go CLI/TUI toolkit for building AI agents and managing LLM-backed workflows.

## Overview

- **TUI Chat Experience**: multi-turn terminal chat with context management and tool execution.
- **LLM Provider Abstraction**: a unified interface for multiple model providers.
- **Agent Runtime**: encapsulates conversation state, tool calls, and execution flow.
- **Harness Proxy Layer**: harness assembles controllers such as context, permissions, memory, skills, and sub-agents, and serves as the execution proxy for the underlying agent.

## Quick Start

The example below shows the current bootstrap flow: build the agent, build the harness, attach the harness to the agent, then execute through the harness proxy. See [`example/prompt_one_with_settings.go`](/Users/wangsj/workspace/acgo/example/prompt_one_with_settings.go).

### 1. Create the settings file

Create `${HOME}/.acgo/settings.yaml`.

```yaml
agent:
 default_provider: gemini
 default_model: gemini-2.5-flash
 providers:
    - provider: gemini
      api_key: ${GEMINI_API_KEY}
    - provider: anthropic
      api_key: ${ANTHROPIC_API_KEY}
log:
 level: debug
```

### 2. Load settings and initialize runtime

```go
settings, err := setting.LoadAndRuntimeInit(
	config.WithDirectName(".acgo"),
	config.WithName("settings"),
)
```

### 3. Build the agent

```go
ag, err := bootstrapagent.BuildAgent(
	settings,
	bootstrapagent.WithId("example-bootstrap-agent"),
	bootstrapagent.WithDefaultTools(),
)
```

### 4. Build and attach the harness

```go
h := bootstrapharness.BuildDefaultHarness()
if err := h.Attach(ag); err != nil {
	return fmt.Errorf("attach harness failed: %w", err)
}
```

### 5. Subscribe through the harness proxy

```go
unsub := h.Subscribe(func(e agent.Event, abort func()) {
	switch e.Type {
	case agent.EventMessageEnd:
		if e.Message == nil {
			return
		}
		fmt.Printf("%s: %s\n", e.Message.Role, e.Message.ContentBlocksToText())
	}
})
defer unsub()
```

### 6. Execute through the harness proxy

```go
userText := "Hello, introduce yourself in one sentence."
if err := h.Prompt(context.Background(), userText); err != nil {
	return fmt.Errorf("prompt failed: %w", err)
}
```

## Assembly Responsibilities

- `pkg/bootstrap/agent`: builds `*agent.Agent` only
- `pkg/bootstrap/harness`: builds `*harness.Harness` only
- `h.Attach(ag)`: performs the final assembly
- `h.Prompt(...)` / `h.Subscribe(...)`: execute through the harness proxy so harness constraints remain in effect

The default TUI follows the same path: build the agent, build the default harness, attach, then call the harness as the public executor.

## Contributing

Before sending changes, read:

- `docs/roadmap/global.md`
- `CONTRIBUTING.md` or `AGENTS.md` if added later

## Development

```bash
go version
go test ./...
go run ./cmd/acgo
```

## Build

```bash
make build
make run
```

## License

MIT
