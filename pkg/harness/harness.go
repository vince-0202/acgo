package harness

import (
	"context"
	"fmt"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/keys"
	"github.com/vince-0202/acgo/pkg/llm"
)

type Controller interface {
	Name() string
	Install(agent agent.AgentRuntime) (func(), error)
}

type Harness struct {
	Controllers []Controller
	uninstalls  []func()
	executor    agent.AgentExecutor
}

func NewHarness(controllers ...Controller) *Harness {
	res := &Harness{
		Controllers: make([]Controller, 0, len(controllers)),
		uninstalls:  make([]func(), 0, len(controllers)),
	}
	res.Controllers = append(res.Controllers, controllers...)
	return res
}

func (h *Harness) Attach(executor agent.AgentExecutor) error {
	if h == nil {
		return nil
	}
	h.executor = executor
	for _, controller := range h.Controllers {
		if controller == nil {
			continue
		}
		uninstall, err := controller.Install(executor)
		if err != nil {
			h.Detach()
			return fmt.Errorf("install controller %q: %w", controller.Name(), err)
		}
		if uninstall == nil {
			uninstall = func() {}
		}
		h.uninstalls = append(h.uninstalls, uninstall)
	}
	return nil
}

func (h *Harness) Detach() {
	if h == nil {
		return
	}
	for i := len(h.uninstalls) - 1; i >= 0; i-- {
		if h.uninstalls[i] != nil {
			h.uninstalls[i]()
		}
	}
	h.uninstalls = h.uninstalls[:0]
	h.executor = nil
}

func (h *Harness) requireExecutor() (agent.AgentExecutor, error) {
	if h == nil || h.executor == nil {
		return nil, fmt.Errorf("harness is not attached to an agent")
	}
	return h.executor, nil
}

func (h *Harness) Prompt(ctx context.Context, content string) error {
	executor, err := h.requireExecutor()
	if err != nil {
		return err
	}
	return executor.Prompt(ctx, content)
}

func (h *Harness) PromptScheduledTask(ctx context.Context, taskDescription string) error {
	executor, err := h.requireExecutor()
	if err != nil {
		return err
	}
	return executor.PromptScheduledTask(ctx, taskDescription)
}

func (h *Harness) Reset() {
	if h == nil || h.executor == nil {
		return
	}
	h.executor.Reset()
}

func (h *Harness) ID() string {
	if h == nil || h.executor == nil {
		return ""
	}
	return h.executor.ID()
}

func (h *Harness) Model() llm.Model {
	if h == nil || h.executor == nil {
		return llm.Model{}
	}
	return h.executor.Model()
}

func (h *Harness) SetModel(model llm.Model) {
	if h == nil || h.executor == nil {
		return
	}
	h.executor.SetModel(model)
}

func (h *Harness) Provider() llm.Provider {
	if h == nil || h.executor == nil {
		return nil
	}
	return h.executor.Provider()
}

func (h *Harness) State() agent.State {
	if h == nil || h.executor == nil {
		return agent.State{}
	}
	return h.executor.State()
}

func (h *Harness) Subscribe(listener agent.Listener) func() {
	if h == nil || h.executor == nil {
		return func() {}
	}
	return h.executor.Subscribe(listener)
}

func (h *Harness) Emit(event agent.Event) {
	if h == nil || h.executor == nil {
		return
	}
	h.executor.Emit(event)
}

func (h *Harness) Abort() {
	if h == nil || h.executor == nil {
		return
	}
	h.executor.Abort()
}

func (h *Harness) SetThinkingLevel(level keys.ThinkingLevel) {
	if h == nil || h.executor == nil {
		return
	}
	h.executor.SetThinkingLevel(level)
}

func (h *Harness) ContextManager() agent.ContextRuntime {
	if h == nil || h.executor == nil {
		return nil
	}
	return h.executor.ContextManager()
}

func (h *Harness) ToolManager() agent.ToolRuntime {
	if h == nil || h.executor == nil {
		return nil
	}
	return h.executor.ToolManager()
}

func (h *Harness) QueueManager() agent.QueueRuntime {
	if h == nil || h.executor == nil {
		return nil
	}
	return h.executor.QueueManager()
}

func (h *Harness) SubAgentManager() agent.SubAgentRuntime {
	if h == nil || h.executor == nil {
		return nil
	}
	return h.executor.SubAgentManager()
}

func (h *Harness) EnqueueSteering(msg communi.Message) {
	if h == nil || h.executor == nil {
		return
	}
	h.executor.EnqueueSteering(msg)
}

func (h *Harness) EnqueueFollowUp(msg communi.Message) {
	if h == nil || h.executor == nil {
		return
	}
	h.executor.EnqueueFollowUp(msg)
}
