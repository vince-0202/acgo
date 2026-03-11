### Pi Monorepo（`badlogic/pi-mono`）分析

#### 总体概览

**`pi-mono` 是一个围绕 “AI 编码代理（coding agent）” 打造的 TypeScript Monorepo**，核心目标是：
- **统一多家 LLM 的调用方式**（`@mariozechner/pi-ai`）
- 在此之上提供通用的 **Agent 运行时**（`@mariozechner/pi-agent-core`）
- 再往上构建 **终端 Coding Agent CLI**（`@mariozechner/pi-coding-agent`，`pi` 命令行工具）
- 配套终端 UI、Web UI、Slack bot、vLLM 部署管理等周边工具

仓库主页和结构见仓库 README（`https://github.com/badlogic/pi-mono`）。

---

#### 核心功能一：统一 LLM API（`@mariozechner/pi-ai`）

参考：`packages/ai/README.md`（`https://github.com/badlogic/pi-mono/tree/main/packages/ai`）

**1. 核心能力**

- **统一封装多家 LLM 服务商**：
  - OpenAI、Azure OpenAI
  - Anthropic
  - Google Gemini、Vertex AI
  - Mistral
  - Groq、Cerebras、xAI
  - Amazon Bedrock
  - OpenRouter、Vercel AI Gateway、zAI 等
  - 各种 OpenAI 兼容 API：Ollama、vLLM、LM Studio、自建代理等
- 定义了一个强类型的 `Model` 抽象：
  - 统一字段：`id`、`name`、`api`、`provider`、`contextWindow`、`maxTokens`、`input`（是否支持 image）、`reasoning`（是否支持思维链）
- 提供两组调用接口：
  - **通用接口**：
    - `stream(model, context, options?)`：流式输出完整事件流
    - `complete(model, context, options?)`：一次性拿到完整回复
  - **简化 reasoning 接口**：
    - `streamSimple/completeSimple(model, context, { reasoning: 'minimal' | 'low' | 'medium' | 'high' | 'xhigh' })`
- **统一的 Tool（函数调用）接口**：
  - 使用 TypeBox (`Type`, `Static`, `TSchema`) 定义工具参数 schema
  - `Tool` 类型：`{ name, description, parameters }`
  - 提供 `validateToolCall(tools, toolCall)` 对工具参数做 AJV 校验
- **细粒度流式事件模型**：
  - 文本相关事件：
    - `start`
    - `text_start` / `text_delta` / `text_end`
  - reasoning/思考事件：
    - `thinking_start` / `thinking_delta` / `thinking_end`
  - 工具调用事件：
    - `toolcall_start` / `toolcall_delta` / `toolcall_end`
  - 收尾事件：
    - `done`（附带 stop reason）
    - `error`（错误或中断）
- **图像输入 & 多模态**：
  - 通过 `model.input.includes('image')` 判断模型是否支持 vision
  - 支持把 base64 图片作为 content block 传入
- **跨 Provider 的上下文迁移（handoff）**：
  - 一个 `Context` 可以先用 Claude，后面换 GPT，再换 Gemini
  - 当切换 provider 时：
    - user/toolResult 消息不变
    - 同一 provider 的 assistant 消息原样保留
    - 不同 provider 的 assistant 消息中 `thinking` block 会被转成带 `<thinking>` 标签的纯文本，以保证兼容性
- **统一的错误与中断处理**：
  - 通过 `AbortController` 中止流式请求
  - `stopReason` 会标记为 `"stop" | "length" | "toolUse" | "error" | "aborted"`
  - 中断时依然返回部分内容和 usage

**2. 核心实现方式**

- **API & Provider 注册表**：
  - 定义 `KnownApi`（如 `anthropic-messages`、`openai-responses`、`google-generative-ai` 等）
  - 为每个 API 提供专门的 `streamXxx` 实现（如 `streamAnthropic`、`streamOpenAICompletions` 等）
  - `getProviders / getModels / getModel` 提供带自动补全的模型枚举和选择
- **统一事件流抽象**：
  - 各 provider 的底层 stream 都被包装成 `AssistantMessageEventStream`
  - 在外层统一转换为标准化的事件类型（文档中有完整表格）
- **Tool 参数的“部分 JSON 解析”**：
  - 在 `toolcall_delta` 阶段就尝试解析目前为止的 JSON 字符串
  - 始终保证 `arguments` 至少是 `{}`（而不是 `undefined`）
  - UI 可以在参数未完全生成前根据已解析字段做“实时预览”
- **OpenAI 兼容层（`compat`）**：
  - 对 `openai-completions` API 增加 `compat` 选项：
    - 是否支持 `store`，是否有 `developer` 角色
    - 是否支持 `reasoning_effort`
    - `maxTokens` 字段名（`max_tokens` vs `max_completion_tokens`）
    - toolResult 是否强制要求 `name` 字段
    - 是否需要在 toolResult 后强制补一个 assistant 消息等
  - 对 Responses API（`openai-responses`）也预留 `compat` 字段以拓展
- **环境变量 & OAuth 集成**：
  - 在 Node 环境自动从环境变量提取各家 provider 的 API key
  - 对 Anthropic/OpenAI Codex/GitHub Copilot/Gemini CLI/Antigravity 等提供 OAuth 登录（`@mariozechner/pi-ai/oauth`）

整体上，`pi-ai` 把“**多 Provider、多模型、多模态、多种工具调用语义**”全部压成一种统一的上下文 + 事件流抽象，是整个系统的基础。

---

#### 核心功能二：Agent 运行时（`@mariozechner/pi-agent-core`）

参考：`packages/agent/README.md`（`https://github.com/badlogic/pi-mono/tree/main/packages/agent`）

**1. 核心能力**

- 提供一个通用的 **`Agent` 类**，整合：
  - LLM 调用（依赖 `@mariozechner/pi-ai`）
  - 工具定义与执行
  - 会话上下文和状态管理
  - 事件流（适合 TUI/Web UI 状态机）
- 区分 **AgentMessage** 与 LLM 消息：
  - Agent 侧的 `AgentMessage` 可以扩展出自定义角色（比如 `notification`）
  - 通过 `convertToLlm(messages)` 把这些消息转换为 LLM 能理解的 `user/assistant/toolResult`
- **消息流 / 轮次（turn）模型**：
  - 一次完整交互：一个 user/toolResult 消息触发一次 LLM 调用 + 所有相关 tool 执行
  - 每一轮内部可再次产生 tool 调用，形成“LLM ↔ 工具”的准循环
- **事件系统**：
  - agent 级别：
    - `agent_start` / `agent_end`：一轮高层调用开始和结束
  - turn 级别：
    - `turn_start` / `turn_end`：一次 LLM + 工具执行周期
  - 消息级别：
    - `message_start` / `message_update` / `message_end`：
      - `message_update` 只用于 assistant 消息，内含 `assistantMessageEvent`（`text_delta` 等）
  - 工具执行级别：
    - `tool_execution_start` / `tool_execution_update` / `tool_execution_end`
- **Steering & Follow-up 消息队列**：
  - `steering` 消息：在工具执行结束后插入，并中断剩余工具
  - `follow-up` 消息：在 agent 完成当前所有任务之后再执行
  - 队列模式支持 `"one-at-a-time"` 或 `"all"`
- **Agent 状态管理**：
  - `AgentState` 包含：
    - `systemPrompt`、`model`、`thinkingLevel`、`tools`、`messages`
    - `isStreaming`、`streamMessage`（流式中的 partial）、`pendingToolCalls`、`error`
  - 提供大量 API 修改/查询：
    - `setSystemPrompt`、`setModel`、`setThinkingLevel`、`setTools`
    - `replaceMessages`、`appendMessage`、`clearMessages`、`reset`
    - `abort`、`waitForIdle`
- 支持自定义：
  - `transformContext(messages, signal)`：在每次 LLM 调用前对上下文做剪枝/注入额外信息
  - `streamFn`: 可把底层 API 调用代理到自定义后端（例如自建 HTTP 服务）
  - `getApiKey(provider)`：动态获取/刷新 OAuth Token

**2. 工具（AgentTool）实现方式**

- `AgentTool` 定义：
  - `name`、`label`、`description`
  - `parameters`：TypeBox schema
  - `execute(toolCallId, params, signal, onUpdate?)`：
    - 返回带 `content` 的对象（文本/图片等）
    - 可以通过 `onUpdate` 流式回调部分结果（对应 `tool_execution_update`）
- 约定：
  - 工具失败时 **抛异常**，不要用 `content` 返回错误信息
  - Agent 捕获异常后，会构造一个 `toolResult`，`isError: true`，交给 LLM 处理

**3. 核心流程概览**

1. 用户调用 `agent.prompt("...")`：
   - 生成一个 `user` 消息
   - 触发 `agent_start` → `turn_start` → 一系列 `message_*` 事件
2. Agent 调用 `transformContext`、`convertToLlm`，再通过 `pi-ai` 的 `stream` 调用模型：
   - 收到 `text_delta`、`toolcall_delta` 等事件时转换成 Agent 的 `message_update` 事件
3. LLM 返回 tool call 时：
   - Agent 依次执行工具 `execute`，产生 `tool_execution_*` 事件
   - 结果被包装为 `toolResult` 消息追加到上下文
4. 若还有新的 tool call 或 steering/follow-up 消息，则开启新一轮 turn

`pi-agent-core` 把“**多轮 LLM 调用 + 工具调用 + 上下文管理 + 事件驱动**”抽象成一个通用、可扩展的 Agent 运行时，不依赖具体的 UI 或 CLI。

---

#### 核心功能三：终端 Coding Agent CLI（`@mariozechner/pi-coding-agent`）

参考：`packages/coding-agent/README.md`（`https://github.com/badlogic/pi-mono/tree/main/packages/coding-agent`）

**1. 功能定位**

- 提供 `pi` 命令行工具，是一个 **面向程序员的终端 Coding Agent**：
  - 用 LLM + 工具调用来读写你的代码仓库、运行命令、搜索、编写/修改代码
  - 主打 **极简核心 + 强扩展能力**：不预置 MCP、子 Agent、Plan 模式等，而是鼓励通过扩展实现
- 运行模式：
  - **交互模式**：TUI，适合日常开发
  - **print/JSON 模式**：用于脚本型调用或集成到其他工具
  - **RPC 模式**：stdin/stdout JSONL 协议，方便非 Node 环境集成
  - **SDK 模式**：以库形式嵌入你自己的应用

**2. 核心特性**

- **内置工具**（默认给模型的工具集）：
  - `read`：读文件
  - `write`：写文件
  - `edit`：对文件做增量修改
  - `bash`：执行 shell 命令
  - `grep` / `find` / `ls`：辅助代码搜索与目录浏览
- **多 Provider / 多模型选择**：
  - 支持订阅登录：
    - Anthropic Claude Pro/Max
    - OpenAI ChatGPT Plus/Pro (Codex)
    - GitHub Copilot
    - Google Gemini CLI
    - Google Antigravity
  - 支持 API key 登录：
    - OpenAI / Anthropic / Azure OpenAI / Gemini / Vertex / Bedrock / Mistral / Groq / Cerebras / xAI / OpenRouter / Vercel AI Gateway / ZAI / OpenCode / HuggingFace / Kimi / MiniMax 等
  - 内置模型列表随每次发布更新，可以用 `/model`、`--provider`、`--model` 切换
- **Session 会话管理**：
  - 所有对话以 JSONL 树的形式保存在 `~/.pi/agent/sessions`
  - 每条消息都有 `id` / `parentId`，支持分支与回溯
  - 提供命令：
    - `/resume`：选择历史 Session 继续
    - `/tree`：可视化会话树，跳到任意节点继续，支持折叠/过滤/书签
    - `/fork`：从当前分支创建一个新 Session 文件
    - `/compact`：手动触发上下文压缩
  - 自动压缩：
    - 当上下文超出模型窗口或接近边界时自动触发，总结老消息保留最近窗口
- **Settings & Context Files**：
  - 设置：
    - 通过 `/settings` 进入 TUI 设置界面
    - 或编辑 `~/.pi/agent/settings.json` 和 `.pi/settings.json`
  - Context 文件：
    - 启动时自动加载多层级 `AGENTS.md` / `CLAUDE.md`：
      - 全局：`~/.pi/agent/AGENTS.md`
      - 从当前目录向上查找父目录中的 `AGENTS.md`
      - 当前目录
    - System Prompt：
      - `.pi/SYSTEM.md`：替换默认系统提示
      - `.pi/APPEND_SYSTEM.md`：在默认系统提示后追加
- **交互式编辑器和命令系统**：
  - 编辑器支持：
    - 输入 `@` 触发文件模糊搜索引用
    - Tab 路径补全
    - Shift+Enter / Ctrl+Enter 多行输入
    - 粘贴图片（支持图片消息）
    - `!cmd` 执行 bash并把输出发给 LLM，`!!cmd` 只执行不发给 LLM
  - `/` 命令系统：
    - 内置诸如 `/login` `/model` `/settings` `/session` `/new` `/name` `/tree` `/copy` `/export` `/share` `/reload` `/quit`
    - 扩展可以注册自定义命令，Skill 以 `/skill:name` 暴露，Prompt Template 也是 `/templatename`
- **消息队列（steering/follow-up）**：
  - Agent 正在执行时：
    - `Enter`：把编辑器内容作为 steering 消息入队
    - `Alt+Enter`：作为 follow-up 消息入队
    - `Escape`：中断并把队列消息放回编辑器
    - `Alt+Up`：从队列取回消息到编辑器
  - 对于需要频繁“打断 / 追加任务”的 coding 流程非常友好

**3. 扩展与自定义能力**

- **Prompt Templates**：
  - 存放在 `~/.pi/agent/prompts/`、`.pi/prompts/` 或 Pi Package 中
  - 通过 `/name` 展开模板，支持简单占位符（如 `{{focus}}`）
- **Skills（Agent Skills 标准）**：
  - 遵从 [agentskills.io](https://agentskills.io) 标准的技能描述
  - 放在 `~/.pi/agent/skills/`、`~/.agents/skills/`、`.pi/skills/`、`.agents/skills/` 或 Pi Package 中
  - 通过 `/skill:name` 调用，或者由 agent 自动加载
  - 本质上是可共享的“工作流/流程说明书”
- **Extensions（最核心的可编程扩展点）**：
  - TypeScript 模块，导出默认函数 `export default function (pi: ExtensionAPI) { ... }`
  - 可以：
    - 注册/替换工具（甚至完全替换内置 `read/write/edit/bash` 等）
    - 注册新命令 `/stats`、`/deploy` 等
    - 监听事件（如 `tool_call`），实现自定义日志、权限控制、Git checkpoint 等
    - 改造 UI：自定义编辑器、添加 header/footer/status line/overlay，做“Claude Code 风格”界面
    - 实现 MCP 客户端、子 Agent、Plan 模式、待办/任务系统等
- **Themes & Pi Packages**：
  - Theme：`~/.pi/agent/themes/`、`.pi/themes/` 或 package 中，支持热重载
  - Pi Package：在 npm/git 仓库中聚合 extensions/skills/prompts/themes，通过 `pi install` 安装
  - `package.json` 中增加 `pi` 字段描述资源路径，实现分享与复用

整体来看，`pi-coding-agent` 是把前两层能力“产品化”为一个终端开发助手，但刻意保持内核简约，把复杂功能都留给扩展生态去实现。

---

#### 核心功能四：终端 UI 框架（`@mariozechner/pi-tui`）

参考：`packages/tui/README.md`（`https://github.com/badlogic/pi-mono/tree/main/packages/tui`）

**1. 核心能力**

- **差分渲染 + 同步输出**：
  - 只重绘变化的行，未变的行不重新输出
  - 使用 CSI 2026（`\x1b[?2026h` / `\x1b[?2026l`）确保屏幕更新原子化，无闪烁
- **组件化 UI 模型**：
  - 所有组件实现接口：
    - `render(width: number): string[]`
    - 可选 `handleInput(data: string)`、`invalidate()`
  - `TUI` 负责：
    - 管理子组件列表
    - 处理终端输入/窗口大小变化
    - 驱动渲染与 focus
- **Overlay 支持**：
  - `tui.showOverlay(component, options)` 在现有内容上方渲染弹层
  - 支持宽高/行列的绝对值或百分比，支持 anchor（如 `center`、`top-right` 等）
  - 支持 `visible(termWidth, termHeight)` 控制在不同终端尺寸下是否显示
  - Overlay handle 提供 `hide/setHidden/focus/unfocus` 等接口
- **内置组件丰富**：
  - 容器 & 布局：
    - `Container`、`Box`（带 padding & 背景）、`Spacer`
  - 文本：
    - `Text`（多行文本，带 padding、背景）
    - `TruncatedText`（单行截断，用于 status line 等）
    - `Markdown`（支持标题/加粗/列表/代码块/blockquote 等，并支持自定义 syntax highlight）
  - 输入：
    - `Input`（单行输入，支持快捷键、单行编辑）
    - `Editor`（多行编辑器，支持 autocomplete、路径补全、粘贴折叠、滚动等）
  - 列表 & 设置：
    - `SelectList`（可滚动列表，支持过滤、选中、取消）
    - `SettingsList`（配置面板，每一项可循环枚举值或弹出子菜单）
  - 反馈：
    - `Loader`（加载动画）
    - `CancellableLoader`（带 Escape 中断、AbortSignal 的 Loader）
  - 图片：
    - `Image`（支持 Kitty/iTerm2 inline image 协议，回退到纯文本占位）
- **输入/快捷键解析**：
  - 提供 `matchesKey(data, Key.xxx)` 工具：
    - `Key.enter`, `Key.escape`, `Key.tab`, `Key.ctrl("c")`, `Key.shift("tab")`, `Key.alt("left")` 等
  - 兼容 Kitty Keyboard Protocol
- **IME 支持（CJK 输入法光标）**：
  - 定义 `Focusable` 接口和 `CURSOR_MARKER`：
    - 组件在 `render` 输出中插入 `CURSOR_MARKER` 用作“软光标”标记
    - TUI 会在渲染后把终端硬光标移动到对应位置，使输入法候选框位置正确
  - 已在 `Editor` 和 `Input` 中实现

**2. 实现细节**

- 所有宽度计算和截断都使用 `visibleWidth`、`truncateToWidth`、`wrapTextWithAnsi`：
  - 正确处理 ANSI 颜色码宽度
  - 截断时会自动补全关闭 SGR，避免样式污染后续行
- 渲染策略：
  1. 首次渲染：全量输出，不清空 scrollback
  2. 终端 width 变化或变更发生在 viewport 之上：清屏 + 全量渲染
  3. 正常更新：定位至首个变化行，清除到末尾，只重绘后续变更部分
- 终端接口抽象：
  - `Terminal` 接口屏蔽了 `stdin/stdout`、`xterm` headless 等不同环境
  - 内置：
    - `ProcessTerminal`：真实终端
    - `VirtualTerminal`：测试环境用

`pi` CLI 的整个界面就是基于 `pi-tui` 这些组件和渲染策略搭建的。

---

#### 其它配套包（简要）

除上述核心包外，仓库还包含：

- **`@mariozechner/pi-mom`**：
  - Slack bot，将 Slack 频道中的消息转发给 pi coding agent，并返回结果
- **`@mariozechner/pi-web-ui`**：
  - Web 前端组件库，快速搭建类 ChatGPT/Claude Code 的 Web 聊天界面
- **`@mariozechner/pi-pods`**：
  - 管理 vLLM GPU pods 的 CLI 工具，用于创建/管理模型部署，结合 `pi-ai` 形成端到端闭环

---

#### 总结：整体架构与实现思路

- **最底层（LLM 统一层：`pi-ai`）**：
  - 通过统一的 Model/Context/事件流抽象，把多 Provider、多模型、多模态和工具调用统一封装起来。
- **中间层（Agent 运行时：`pi-agent-core`）**：
  - 把“消息历史 + 模型 + 工具 + 多轮调用 + Steering/Follow-up + 错误处理”抽象成一个通用 Agent，引入事件流接口方便 UI。
- **上层产品（终端 Coding Agent：`pi-coding-agent`）**：
  - 在 Agent 之上结合 TUI 框架和一套默认工具，形成可交互的终端编码助手，并提供会话、模型、context 文件、扩展体系等完整开发体验。
- **UI 基础设施（`pi-tui`）**：
  - 独立的终端 UI 框架，实现高性能无闪烁渲染和组件化交互，为 `pi` 提供 UI 底座，也可以用于其他 CLI 应用。

整体设计思路是：**将“统一 LLM 调用 → Agent 运行时 → 终端/Web/Slack 等面向用户的界面”分层解耦，每一层都可单独使用和扩展**。对于使用者而言，你既可以仅使用 `@mariozechner/pi-ai` 当作统一 LLM SDK，也可以使用 `@mariozechner/pi-agent-core` 搭建自己的 Agent 系统，或者直接使用 `pi` 作为现成的终端 Coding Agent，并通过扩展完全定制其行为。

