package runtime

import (
	"errors"
	"sync"

	"github.com/vince-0202/acgo/pkg/agent"
)

var (
	agentRegistry = map[string]*agent.Agent{}
	agentMu       sync.RWMutex
)

func RegisterAgent(a *agent.Agent) error {
	if a == nil {
		return errors.New("nil agent")
	}
	agentMu.Lock()
	defer agentMu.Unlock()
	if _, ok := agentRegistry[a.ID()]; ok {
		return errors.New(a.ID() + " agent already exists")
	}
	agentRegistry[a.ID()] = a
	return nil
}

// ListAgents returns all registered agent_new runtimes.
func ListAgents() []*agent.Agent {
	agentMu.RLock()
	defer agentMu.RUnlock()
	out := make([]*agent.Agent, 0, len(agentRegistry))
	for _, p := range agentRegistry {
		out = append(out, p)
	}
	return out
}

// GetAgent returns a registered agent_new runtime by ID.
func GetAgent(id string) (*agent.Agent, bool) {
	agentMu.RLock()
	defer agentMu.RUnlock()
	a, ok := agentRegistry[id]
	return a, ok
}
