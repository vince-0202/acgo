package runtime

import (
	"acgo/pkg/agent"
	"acgo/pkg/llm"
	"errors"
)

var (
	agentRegistry = map[string]*agent.Agent{}
)

func RegisterAgent(a *agent.Agent) error {
	if a == nil {
		return errors.New("nil agent")
	}
	if _, ok := agentRegistry[a.Id()]; ok {
		return errors.New(a.Id() + " agent already exists")
	}
	agentRegistry[a.Id()] = a
	return nil
}

// ListAgents returns all registered agents.
func ListAgents() []*agent.Agent {
	out := make([]*agent.Agent, 0, len(agentRegistry))
	for _, p := range agentRegistry {
		out = append(out, p)
	}
	return out
}

// DefaultStreamFn looks up the provider for the given model and calls its Stream function.
func DefaultStreamFn(callCtx llm.Context, model llm.Model, opts *llm.Options) (<-chan llm.Event, error) {
	provider, ok := GetProvider(model.Provider)
	if !ok {
		ch := make(chan llm.Event, 1)
		ch <- llm.Event{
			Type:  llm.EventError,
			Error: llm.ErrUnknownProvider(model.Provider),
		}
		close(ch)
		return ch, nil
	}
	return provider.Stream(callCtx, model, opts)
}
