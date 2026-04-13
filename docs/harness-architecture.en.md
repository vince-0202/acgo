# From Chaos to Clarity by Introducing Harness Constraints and an Event-Driven Architecture: Refactoring an AI Coding Tool Based on Golang

Over the past few weeks, I have been building an AI coding tool in Go. The core loop is straightforward: accept a natural-language instruction, let the LLM interpret intent, then execute coding work through tools such as file read/write, code search, and terminal commands.

As of now, I haven't come across any agent coding tools written in Go, but I have always thought that Go is an excellent language and is very suitable for building any CLI tools.

The first version moved fast. The trouble started when I tried to add real operational constraints:

- limit how many tools can be called
- block dangerous commands
- record execution traces
- control context growth
- keep child agents inside the same boundaries

My first implementation was the most obvious one: put constraint logic directly into the Agent path.

```go
// Pseudocode: constraints mixed directly into the Agent
func (a *Agent) CallTool(toolName string, args map[string]any) (any, error) {
    if a.toolCallCount > 10 {
        return nil, errors.New("tool call limit exceeded")
    }
    if toolName == "exec" && strings.Contains(args["command"].(string), "rm -rf") {
        return nil, errors.New("dangerous command blocked")
    }
    logToHarness(toolName, args)
    // ... original execution logic ...
}
```

The result was predictable. Constraint logic quickly became entangled with dialogue flow, tool execution, state management, and error handling. Every new rule polluted the hot path a little more. The core problem was no longer implementation discipline. The architecture itself had the wrong boundary.

## Refactoring Goal

I reset the design around three principles:

1. Keep the Agent kernel focused on conversation flow, tool orchestration, and state updates.
2. Treat constraints as external capabilities wired in through listeners or middleware.
3. Isolate LLM providers behind a stable abstraction so upper layers do not care about vendor-specific APIs.

In the current codebase, this becomes a clean three-layer structure:

```text
┌─────────────────────────────────────────────┐
│                Harness Layer                 │
│  Context / Skills / Permission / Memory     │
│  Sub-agent orchestration                    │
├─────────────────────────────────────────────┤
│                 Agent Kernel                 │
│  Prompt / turn / queue / tool execution     │
│  listener events + tool middleware hooks    │
├─────────────────────────────────────────────┤
│                LLM Provider Layer            │
│     OpenAI / Anthropic / Gemini / Qwen      │
└─────────────────────────────────────────────┘
```

The rest of this post maps that structure to the actual implementation in this repository.

## LLM Provider Layer: one interface across vendors

The bottom layer hides provider-specific details. In this project, that abstraction lives in [pkg/llm/provider.go](/Users/wangsj/workspace/acgo/pkg/llm/provider.go):

```go
type Provider interface {
    Name() string
    Stream(ctx context.Context, model Model, message []communi.Message, opts *Options) (<-chan communi.LLMEvent, error)
    Complete(ctx context.Context, model Model, message []communi.Message, opts *Options) (communi.Message, Usage, error)
    Models() []Model
}
```

The value of this layer is practical. The Agent does not need to know about transport details, authentication, streaming formats, or model enumeration differences. It only depends on `llm.Provider`. That makes it possible to switch between OpenAI, Anthropic, Gemini, Qwen, and later local models without rewriting the orchestration logic above.

To the Agent, an LLM is simply a capability boundary: given messages and tool schemas, return the next model response.

## Agent Kernel: conversation flow and tool execution only

The most important part of the refactor was not adding features to the Agent. It was removing responsibilities from it.

The `Agent` implementation lives in [pkg/agent/agent.go](/Users/wangsj/workspace/acgo/pkg/agent/agent.go), and its minimal runtime surface is defined in [pkg/agent/runtime.go](/Users/wangsj/workspace/acgo/pkg/agent/runtime.go). Its core responsibilities are intentionally small:

- manage context, turns, and queues
- call the provider for model output
- execute tools and feed results back into the conversation

The runtime interface makes that boundary explicit:

```go
type AgentRuntime interface {
    ID() string
    Model() llm.Model
    Provider() llm.Provider
    State() State

    Subscribe(Listener) func()
    Emit(Event)
    Abort()
    SetThinkingLevel(keys.ThinkingLevel)

    ContextManager() ContextRuntime
    ToolManager() ToolRuntime
    QueueManager() QueueRuntime
    SubAgentManager() SubAgentRuntime
}
```

The Harness can observe and extend the Agent through this surface, without reaching into internal execution details.

### Event listeners: clean lifecycle cut points

Instead of baking policies into the kernel, the Agent emits lifecycle events. The event types are defined in [pkg/agent/events.go](/Users/wangsj/workspace/acgo/pkg/agent/events.go), including:

- `EventAgentStart`
- `EventAgentEnd`
- `EventTurnStart`
- `EventTurnEnd`
- `EventBeforeLLMCall`
- `EventAfterLLMCall`
- `EventToolExecutionStart`
- `EventToolExecutionUpdate`
- `EventToolExecutionEnd`
- `EventAfterToolExecution`

The listener API in [pkg/agent/listener.go](/Users/wangsj/workspace/acgo/pkg/agent/listener.go) is intentionally simple:

```go
type Listener func(event Event, abort func())
```

That is enough for external controllers to:

- observe every LLM call
- observe every tool execution
- abort a running flow when needed
- update external state based on execution outcomes

This is a natural fit for cross-cutting concerns like context management, auditing, telemetry, or prompt shaping.

### Tool middleware: move pre-execution policy out of the hot path

Events are useful, but they are not sufficient for policy enforcement. Some constraints must run before a tool executes and must be able to reject the request synchronously. That is why the project also exposes a middleware cut point in [pkg/agent/tools.go](/Users/wangsj/workspace/acgo/pkg/agent/tools.go):

```go
type ToolExecutionMiddleware func(ctx context.Context, req ToolExecutionRequest, next ToolExecutionHandler) (communi.ToolCallResult, error)

type ToolRuntime interface {
    RegisterMiddleware(ToolExecutionMiddleware) func()
    // ...
}
```

This is the right place for permission checks. Those checks are execution policy, not tool business logic. The Agent kernel answers "how do we execute this tool call?" The Harness answers "are we allowed to execute it like this?"

## Harness Layer: compose cross-cutting constraints outside the kernel

The core Harness types live in [pkg/harness/harness.go](/Users/wangsj/workspace/acgo/pkg/harness/harness.go):

```go
type Controller interface {
    Name() string
    Install(agent agent.AgentRuntime) (func(), error)
}

type Harness struct {
    Controllers []Controller
    uninstalls  []func()
}
```

This design decision matters. A controller is not forced into a large fixed hook interface. Instead, each controller decides how it wants to integrate during `Install(...)`. It can:

- subscribe to events
- register tool middleware
- mutate context
- register additional tools
- propagate the same boundaries to child agents

That is more flexible than predefining dozens of callbacks, and it fits idiomatic Go composition much better.

Harness attachment is also minimal:

```go
func (h *Harness) Attach(agent agent.AgentRuntime) error {
    for _, controller := range h.Controllers {
        uninstall, err := controller.Install(agent)
        if err != nil {
            h.Detach()
            return err
        }
        h.uninstalls = append(h.uninstalls, uninstall)
    }
    return nil
}
```

Each controller remains independently testable and independently replaceable.

## How the current controllers work

The repository already splits several typical constraints into dedicated controllers.

### 1. ContextController: context trimming and prompt loading

[pkg/harness/context.go](/Users/wangsj/workspace/acgo/pkg/harness/context.go) implements `ContextController`, which mainly does two things:

- load prompt material from `SYSTEM.md` and `AGENTS.md`
- trim or compact context before LLM calls

It integrates through event subscription:

```go
unsub := runtime.Subscribe(func(event agent.Event, abort func()) {
    switch event.Type {
    case agent.EventAgentStart:
        cc.loadPromptFromDisk()
    case agent.EventBeforeLLMCall:
        _ = cc.TrimMessage(context.Background(), nil)
    }
})
```

This is a textbook cross-cutting concern. It changes the Agent's input environment without complicating the Agent's core control flow.

### 2. PermissionController: enforce execution policy in middleware

[pkg/harness/permission.go](/Users/wangsj/workspace/acgo/pkg/harness/permission.go) uses middleware rather than events:

```go
uninstall := runtime.ToolManager().RegisterMiddleware(
    func(ctx context.Context, req agent.ToolExecutionRequest, next agent.ToolExecutionHandler) (communi.ToolCallResult, error) {
        _, err := pc.Check(ctx, PermissionRequest{
            Action:   "tool.execute",
            Resource: "tool:" + req.Tool.Name(),
            Metadata: map[string]any{
                "tool_name": req.Tool.Name(),
                "tool_args": strings.TrimSpace(string(req.Args)),
            },
        })
        if err != nil {
            res := communi.ErrorToolCallResult(req.ToolCall.ID, err)
            return res, err
        }
        return next(ctx, req)
    },
)
```

This completely removes permission decisions from both the Agent kernel and individual tool implementations.

It also supports multiple execution modes:

- `default`
- `acceptEdits`
- `plan`
- `auto`
- `bypassPermissions`

That means the same Agent kernel can be reused across a CLI, a desktop app, semi-automatic flows, and fully automatic flows without duplicating the orchestration logic.

### 3. SkillsController: treat skills as a prompt-extension layer

[pkg/harness/skills.go](/Users/wangsj/workspace/acgo/pkg/harness/skills.go) scans system-level and project-level `SKILL.md` files, then appends the discovered skill guidance into the system prompt.

If this lived inside the Agent, the Agent would need to understand what a skill is, where to load it from, and how overrides work. That would be the wrong abstraction boundary. Inside the Harness, the Agent only sees the resolved prompt.

### 4. SubAgentController: keep child agents inside the same boundaries

The part that made this refactor feel truly worth it is [pkg/harness/subagent_controller.go](/Users/wangsj/workspace/acgo/pkg/harness/subagent_controller.go).

It not only registers the `sub_agent` tool. It also ensures that child agents are created with a matching set of controllers:

```go
factories := []ChildControllerFactory{
    func() Controller { return NewContextController() },
    func() Controller { return NewSkillsController() },
    func() Controller { return permission.Clone() },
    func() Controller { return NewMemoryController(opts.MemoryWriter) },
}
```

This solves a common failure mode in agent systems: the parent agent has carefully designed boundaries, while delegated child agents run without the same safeguards.

Without a Harness layer, permission mode, context compaction, skill loading, and memory policies tend to be copied manually and drift over time. With the Harness, this becomes a consistent assembly problem instead of repeated custom wiring.

## Default assembly: make common boundaries available out of the box

[pkg/harness/default.go](/Users/wangsj/workspace/acgo/pkg/harness/default.go) provides a default composition:

```go
return NewHarness(
    NewContextController(),
    NewSkillsController(),
    permission,
    NewMemoryController(opts.MemoryWriter),
    NewSubAgentController(...),
)
```

[pkg/bootstrap/harness/harness_build.go](/Users/wangsj/workspace/acgo/pkg/bootstrap/harness/harness_build.go) exposes that composition through a stable bootstrap entry point.

In practice, that gives callers a ready-made boundary package with:

- context management
- skill discovery
- permission enforcement
- memory writes
- sub-agent control

When a new capability is needed, the system grows by adding another `Controller`, not by reopening the Agent core.

## What this refactor actually improved

Looking back, the biggest gain was not elegance. It was recoverable system evolution.

### 1. The Agent kernel became understandable again

The Agent now focuses on dialogue progression and execution mechanics. If there is a turn-flow issue, inspect the Agent. If there is a permission, context, or skill issue, inspect the matching controller. The search space is much smaller.

### 2. Constraints became independently testable and replaceable

The permission controller does not need to know how context trimming works. The skills controller does not need to understand child-agent creation. Each one owns a single cross-cutting concern, and the failure domain is clearer.

### 3. Adding new behavior got cheaper

Future additions such as:

- an audit controller
- a tool-call quota controller
- a Prometheus metrics controller
- a user-confirmation interceptor

do not require changes to the Agent hot path anymore. If the behavior can be expressed through listeners or middleware, it can remain external.

### 4. Child-agent architecture finally matches parent-agent architecture

A lot of agent systems remain reasonably clean in the single-agent phase, then become inconsistent once delegation appears. The Harness-based propagation model prevents that drift.

## Conclusion

The shift from "put constraints directly inside the Agent" to "use a dedicated Harness layer" is really a return to a familiar engineering principle: cross-cutting concerns should not be coupled to the core execution path.

In this project, that principle translates into a practical split:

- the provider layer owns model-vendor differences
- the Agent kernel owns dialogue flow and tool orchestration
- the Harness owns permissions, context, skills, memory, and child-agent boundaries
- events and middleware provide the decoupling points

If you are building an AI coding agent and you already feel the core path filling up with guards, audits, prompts, and execution policies, extracting those concerns into a Harness layer is usually cheaper than continuing to patch the kernel.

The code snippets here are simplified, but the architecture matches the current repository. A good starting path is:

- [pkg/llm/provider.go](/Users/wangsj/workspace/acgo/pkg/llm/provider.go)
- [pkg/agent/runtime.go](/Users/wangsj/workspace/acgo/pkg/agent/runtime.go)
- [pkg/agent/events.go](/Users/wangsj/workspace/acgo/pkg/agent/events.go)
- [pkg/agent/tools.go](/Users/wangsj/workspace/acgo/pkg/agent/tools.go)
- [pkg/harness/harness.go](/Users/wangsj/workspace/acgo/pkg/harness/harness.go)
- [pkg/harness/context.go](/Users/wangsj/workspace/acgo/pkg/harness/context.go)
- [pkg/harness/permission.go](/Users/wangsj/workspace/acgo/pkg/harness/permission.go)
- [pkg/harness/skills.go](/Users/wangsj/workspace/acgo/pkg/harness/skills.go)
- [pkg/harness/subagent_controller.go](/Users/wangsj/workspace/acgo/pkg/harness/subagent_controller.go)
