package agent

import (
	"context"

	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/llm"
)

// AgentRuntime is the minimal control surface exposed to harness controllers.
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

// AgentExecutor is the executable agent abstraction exposed to callers.
// A concrete agent can implement it directly, and harness can proxy it.
type AgentExecutor interface {
	AgentRuntime

	Prompt(context.Context, string) error
	PromptScheduledTask(context.Context, string) error
	Reset()
	Abort()

	SetModel(llm.Model)
	EnqueueSteering(communi.Message)
	EnqueueFollowUp(communi.Message)
}

// ContextRuntime exposes the mutable conversation context owned by the agent.
type ContextRuntime interface {
	WorkDir() string
	ProjectRoot() string
	ToolWorkingDirectory() string
	SystemPrompt() string
	PersistentPrompts() []string
	ContextOptions() *ContextOptions
	MessageSnapshot() []communi.Message
	LoadedPaths() []string

	SetProjectRoot(string) error
	ReplacePrompt(string)
	AppendPrompt(string)
	AppendPersistentPrompt(string)
	UpsertPersistentPrompt(string, string)
	RemovePersistentPrompt(string)
	SetLoadedPaths([]string)

	AppendMessage(...communi.Message)
	ReplaceMessages([]communi.Message)
	ClearMessages()
}

// ToolRuntime exposes the agent-owned tool registry and execution pipeline.
type ToolRuntime interface {
	RegisterTool(Tool)
	UnregisterTool(name string)
	FindTool(name string) (Tool, bool)
	RegisteredTools() []Tool
	GetToolSchemas() []communi.ToolSchema

	UpsertPendingToolCall(communi.ToolCallRequest)
	GetPendingToolCalls() []communi.ToolCallRequest
	ClearPendingTool()

	RegisterMiddleware(ToolExecutionMiddleware) func()
}

// QueueRuntime exposes deferred steering/follow-up messages.
type QueueRuntime interface {
	EnqueueSteering(communi.Message)
	EnqueueFollowUp(communi.Message)
	DrainOneFromQueues() *communi.Message
	Clear()
}

// SubAgentRuntime exposes child-agent lifecycle operations.
type SubAgentRuntime interface {
	SetFactory(SubAgentFactory)
	Register(string, SubAgentSpec, *Agent) error
	Create(context.Context, SubAgentSpec) (string, error)
	Get(string) (*Agent, bool)
	List() []SubAgentInfo
	Remove(string) error
	Run(context.Context, string, string) (string, error)
	DispatchTask(context.Context, DispatchRequest) (DispatchDecision, error)
	SendMessage(fromSubID, toSubID, intent, payload, correlationID string, metadata map[string]any) (MessageEnvelope, error)
	PullInbox(subID string, limit int, correlationID string) []MessageEnvelope
	AckMessage(messageID string) (MessageEnvelope, error)
}

// Options configures a new minimal agent runtime.
type Options struct {
	ID            string
	WorkDir       string
	Model         llm.Model
	Provider      llm.Provider
	Tools         []Tool
	InitialState  State
	InitialPrompt string
}

// New constructs an agent runtime with only the core execution managers wired in.
func New(opts Options) *Agent {
	toolMap := make(map[string]Tool, len(opts.Tools))
	for _, tool := range opts.Tools {
		if tool == nil {
			continue
		}
		toolMap[tool.Name()] = tool
	}

	state := opts.InitialState
	if state.WorkDir == "" {
		state.WorkDir = opts.WorkDir
	}

	ctxMgr := &Context{
		workDir:  opts.WorkDir,
		Messages: make([]communi.Message, 0),
		Options:  &ContextOptions{},
		Paths:    make([]string, 0),
		Prompt:   defaultSystemPrompt,
	}
	if opts.InitialPrompt != "" {
		ctxMgr.Prompt = opts.InitialPrompt
	}

	agent := &Agent{
		id:       opts.ID,
		model:    opts.Model,
		provider: opts.Provider,
		state:    state,
		context:  ctxMgr,
		toolManager: &toolsManager{
			tools:            toolMap,
			pendingToolCalls: make([]communi.ToolCallRequest, 0),
			middlewares:      make([]ToolExecutionMiddleware, 0),
		},
		turnManager: &turnManager{},
		listenerManager: &listenerManager{
			listeners: make([]listenerSlot, 0),
		},
		queueManager: &queueManager{
			SteeringQueue: make([]communi.Message, 0),
			FollowUpQueue: make([]communi.Message, 0),
		},
	}
	agent.subAgentManager = NewSubAgentManager(agent)
	return agent
}
