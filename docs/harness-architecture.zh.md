# 从混乱到清晰：基于 Golang 的 AI Coding 工具重构之路

过去几周，我一直在用 Golang 构建一款 AI Coding 工具。它的核心能力很直接：接收自然语言指令，经由 LLM 理解意图，再调用文件读写、代码搜索、终端命令执行等工具完成任务。

第一版推进得很快，但当我开始补齐约束能力时，问题集中爆发了。比如：

- 限制工具调用次数
- 拦截危险命令
- 记录执行轨迹
- 控制上下文大小
- 给子 Agent 继承一致的边界

一开始我也走过那条最“顺手”的路：把这些逻辑直接塞进 Agent 核心路径。

```go
// 伪代码：把约束直接混进 Agent 的执行路径
func (a *Agent) CallTool(toolName string, args map[string]any) (any, error) {
    if a.toolCallCount > 10 {
        return nil, errors.New("tool call limit exceeded")
    }
    if toolName == "exec" && strings.Contains(args["command"].(string), "rm -rf") {
        return nil, errors.New("dangerous command blocked")
    }
    logToHarness(toolName, args)
    // ... 原有执行逻辑 ...
}
```

结果并不意外。约束逻辑很快和对话流程、工具执行、状态管理、错误处理缠在一起。每加一个规则，就要继续污染主路径；不同约束之间还会互相影响，最后连原本稳定的多轮交互都开始变得难以推断。

问题不是“代码再整理一下就好”，而是架构边界本身出了问题。

## 重构目标

这次重构我给自己定了三条原则：

1. Agent 内核保持纯粹，只负责对话推进、工具调度和状态变更。
2. 约束作为外部能力存在，通过事件监听或中间件接入，而不是写死在内核里。
3. LLM 提供层独立抽象，避免上层感知不同厂商的调用差异。

最终，在当前项目里形成了这样一套分层：

```text
┌─────────────────────────────────────────────┐
│                Harness 层                    │
│  Context / Skills / Permission / Memory     │
│  SubAgent orchestration                     │
├─────────────────────────────────────────────┤
│               Agent 内核层                   │
│  Prompt / turn / queue / tool execution     │
│  listener events + tool middleware hooks    │
├─────────────────────────────────────────────┤
│                LLM 提供层                    │
│     OpenAI / Anthropic / Gemini / Qwen      │
└─────────────────────────────────────────────┘
```

下面结合仓库里的真实代码说明每一层分别承担什么职责。

## LLM 提供层：统一不同厂商的调用接口

最底层先解决的是模型接入问题。在当前代码里，这层抽象定义在 [pkg/llm/provider.go](/Users/wangsj/workspace/acgo/pkg/llm/provider.go)：

```go
type Provider interface {
    Name() string
    Stream(ctx context.Context, model Model, message []communi.Message, opts *Options) (<-chan communi.LLMEvent, error)
    Complete(ctx context.Context, model Model, message []communi.Message, opts *Options) (communi.Message, Usage, error)
    Models() []Model
}
```

它的价值不在“接口写得多漂亮”，而在于上层完全不需要知道每个厂商的 HTTP 协议、鉴权方式、流式事件格式、模型枚举差异。Agent 只依赖 `llm.Provider`，因此切换 OpenAI、Anthropic、Gemini、Qwen，甚至后面补本地模型时，不需要重写调度逻辑。

对 Agent 来说，LLM 是一个“给定消息和工具定义，返回模型响应”的能力边界，而不是某个供应商 SDK。

## Agent 内核层：只做对话推进与工具执行

这次重构的关键，不是给 Agent 加更多能力，而是有意识地给它减负。

当前项目里的 `Agent` 定义在 [pkg/agent/agent.go](/Users/wangsj/workspace/acgo/pkg/agent/agent.go)，最小运行时接口定义在 [pkg/agent/runtime.go](/Users/wangsj/workspace/acgo/pkg/agent/runtime.go)。核心职责可以概括为三件事：

- 管理上下文、回合与队列
- 调用 Provider 获取模型输出
- 执行工具并把结果反馈回对话

`agent.New(...)` 构建出来的是一个尽量薄的运行时：

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

这个接口本身已经说明了边界：Harness 能观察和扩展 Agent，但不需要侵入它的内部实现细节。

### 事件监听点：给外层控制留出清晰切口

Agent 不再直接内建各种策略，而是发布生命周期事件。事件类型定义在 [pkg/agent/events.go](/Users/wangsj/workspace/acgo/pkg/agent/events.go)，包括：

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

监听器接口也很简单，定义在 [pkg/agent/listener.go](/Users/wangsj/workspace/acgo/pkg/agent/listener.go)：

```go
type Listener func(event Event, abort func())
```

也就是说，外层控制器可以在不改 Agent 主流程的情况下：

- 观测每一次 LLM 调用
- 观测每一次工具执行
- 在必要时中断当前流程
- 根据事件结果更新外部状态

这类监听点非常适合上下文整理、审计、埋点、提示词追加等横切逻辑。

### 工具中间件：把“执行前拦截”从主路径剥离出去

仅靠事件还不够，因为权限拦截这类能力通常要求“在工具真正执行前就能拒绝请求”。因此项目把另一个切口放在工具执行链上。

相关定义在 [pkg/agent/tools.go](/Users/wangsj/workspace/acgo/pkg/agent/tools.go)：

```go
type ToolExecutionMiddleware func(ctx context.Context, req ToolExecutionRequest, next ToolExecutionHandler) (communi.ToolCallResult, error)

type ToolRuntime interface {
    RegisterMiddleware(ToolExecutionMiddleware) func()
    // ...
}
```

这比把权限判断写进某个具体工具里更合理，因为它属于统一的执行策略，不属于工具本身的业务逻辑。

换句话说，Agent 内核负责“如何执行”，Harness 负责“是否允许这样执行”。

## Harness 层：把横切约束聚合成可组合能力

在当前仓库中，Harness 的核心定义在 [pkg/harness/harness.go](/Users/wangsj/workspace/acgo/pkg/harness/harness.go)：

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

这里最重要的设计点是：`Controller` 不是要求实现一堆固定回调，而是通过 `Install(...)` 自己决定如何接入 Agent。它可以：

- 订阅事件
- 注册工具中间件
- 修改上下文
- 注册额外工具
- 为子 Agent 注入同样的控制器

这种做法比“预先定义几十个 hook 接口”更灵活，也更贴合 Go 里组合优于继承的风格。

Harness 的挂载也很直接：

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

这让每个控制器既能独立演进，也能按需组合。

## 现在这套控制器是怎么工作的

项目里已经把几类典型约束拆成了独立控制器。

### 1. ContextController：上下文裁剪与 Prompt 注入

[pkg/harness/context.go](/Users/wangsj/workspace/acgo/pkg/harness/context.go) 里的 `ContextController` 主要做两件事：

- 在启动时加载 `SYSTEM.md`、`AGENTS.md` 等提示信息
- 在 `EventBeforeLLMCall` 前自动整理上下文，必要时做 compact

它通过订阅 Agent 事件接入：

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

这类逻辑非常典型：它改变 Agent 的“输入环境”，但不应该污染 Agent 的主执行流程。

### 2. PermissionController：把权限校验放进中间件

[pkg/harness/permission.go](/Users/wangsj/workspace/acgo/pkg/harness/permission.go) 的 `PermissionController` 不是靠事件，而是直接注册工具执行中间件：

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

这一步把“是否允许执行工具”的决策，从 Agent 核心和工具实现里完全抽离了出来。

同时它还支持多种模式：

- `default`
- `acceptEdits`
- `plan`
- `auto`
- `bypassPermissions`

这意味着同一个 Agent 内核，可以在 CLI、桌面端、半自动模式、全自动模式之间复用，而不需要为每种运行环境复制一套执行逻辑。

### 3. SkillsController：把技能系统作为 Prompt 扩展层

[pkg/harness/skills.go](/Users/wangsj/workspace/acgo/pkg/harness/skills.go) 负责扫描系统级和项目级 `SKILL.md`，然后把技能说明追加到系统提示词中。

这类能力如果直接做到 Agent 里，会让 Agent 理解“什么是 skill、从哪里加载、如何覆盖”，明显越界。放在 Harness 后，Agent 只看到整理后的 Prompt。

### 4. SubAgentController：给子 Agent 复制同样的边界

真正让我觉得这套拆分值得的地方，是 [pkg/harness/subagent_controller.go](/Users/wangsj/workspace/acgo/pkg/harness/subagent_controller.go)。

它不仅能注册 `sub_agent` 工具，还能在创建子 Agent 时，为子 Agent 自动挂上一组控制器：

```go
factories := []ChildControllerFactory{
    func() Controller { return NewContextController() },
    func() Controller { return NewSkillsController() },
    func() Controller { return permission.Clone() },
    func() Controller { return NewMemoryController(opts.MemoryWriter) },
}
```

这件事非常重要，因为它解决了一个常见问题：主 Agent 有边界，子 Agent 却“裸奔”。

如果没有 Harness 层，子 Agent 的权限模式、上下文压缩策略、技能注入、记忆策略，通常都得手工复制；而且主子 Agent 很容易逐渐漂移。现在它们变成了统一的装配问题。

## 默认装配：把常用约束变成开箱即用

[pkg/harness/default.go](/Users/wangsj/workspace/acgo/pkg/harness/default.go) 提供了默认组合：

```go
return NewHarness(
    NewContextController(),
    NewSkillsController(),
    permission,
    NewMemoryController(opts.MemoryWriter),
    NewSubAgentController(...),
)
```

而 [pkg/bootstrap/harness/harness_build.go](/Users/wangsj/workspace/acgo/pkg/bootstrap/harness/harness_build.go) 负责给上层提供更稳定的构建入口。

这意味着调用方大多数时候不用手写一堆装配代码，就能获得：

- 上下文管理
- 技能发现
- 权限控制
- 记忆写入
- 子 Agent 管控

如果要扩展新能力，也只需要追加一个新的 `Controller`。

## 这次重构真正带来了什么

回头看，这次重构最核心的收益不是“代码更优雅”，而是系统终于恢复了可演进性。

### 1. Agent 内核重新变得可理解

现在 Agent 内核聚焦于对话与执行本身，不再背负越来越多的策略分支。要排查问题时，先看 Agent 是否正确推进回合；要看权限、上下文或技能行为，则去对应的 Controller。

这种边界让问题定位快了很多。

### 2. 约束能力可以独立测试、独立替换

权限控制器不需要知道上下文裁剪怎么做，SkillsController 也不需要理解子 Agent 的创建过程。每个控制器只处理自己的横切职责，组合顺序明确，故障范围也更可控。

### 3. 新能力的接入成本显著下降

如果后面我要加：

- 审计日志控制器
- 工具调用限次控制器
- Prometheus 性能指标控制器
- 用户确认拦截器

都不需要再去改 Agent 的核心路径。只要能通过事件或中间件接入，就可以独立实现和装配。

### 4. 子 Agent 架构终于和主 Agent 保持一致

很多 Agent 系统在单 Agent 阶段设计还算整洁，一旦引入 delegation，边界就会迅速失控。现在 Harness 可以把同一套约束传播到子 Agent，这一点对长期演进非常关键。

## 总结

这次从“把约束直接写进 Agent”到“拆出 Harness 层”，本质上是在重新尊重一个很经典的工程原则：横切关注点不要和核心执行路径耦合。

在这个项目里，我最终得到的经验是：

- LLM Provider 负责模型接入差异，不要把厂商细节泄漏到 Agent。
- Agent 内核负责对话推进和工具调度，不要背负策略拼装。
- Harness 负责权限、上下文、技能、记忆、子 Agent 边界这些横切能力。
- 事件和中间件是最适合这类系统的解耦切口。

如果你也在构建类似的 AI Coding Agent，尤其当你已经开始往核心路径里塞越来越多的限制、审计和守卫逻辑时，尽早把这些能力抽成 Harness，会比继续在主流程上打补丁划算得多。

最后说明一下：文中的代码片段有简化，但设计方向和当前仓库实现是一致的。你可以直接从这些文件开始看：

- [pkg/llm/provider.go](/Users/wangsj/workspace/acgo/pkg/llm/provider.go)
- [pkg/agent/runtime.go](/Users/wangsj/workspace/acgo/pkg/agent/runtime.go)
- [pkg/agent/events.go](/Users/wangsj/workspace/acgo/pkg/agent/events.go)
- [pkg/agent/tools.go](/Users/wangsj/workspace/acgo/pkg/agent/tools.go)
- [pkg/harness/harness.go](/Users/wangsj/workspace/acgo/pkg/harness/harness.go)
- [pkg/harness/context.go](/Users/wangsj/workspace/acgo/pkg/harness/context.go)
- [pkg/harness/permission.go](/Users/wangsj/workspace/acgo/pkg/harness/permission.go)
- [pkg/harness/skills.go](/Users/wangsj/workspace/acgo/pkg/harness/skills.go)
- [pkg/harness/subagent_controller.go](/Users/wangsj/workspace/acgo/pkg/harness/subagent_controller.go)
