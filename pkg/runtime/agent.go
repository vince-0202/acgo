package runtime

import (
	"errors"
	"github.com/vince-0202/acgo/pkg/agent"
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
