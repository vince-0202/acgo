package runtime

import (
	"errors"
	"github.com/vince-0202/acgo/pkg/agent"
	"sync"
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
	if _, ok := agentRegistry[a.Id()]; ok {
		return errors.New(a.Id() + " agent already exists")
	}
	agentRegistry[a.Id()] = a
	return nil
}

// ListAgents returns all registered agents.
func ListAgents() []*agent.Agent {
	agentMu.RLock()
	defer agentMu.RUnlock()
	out := make([]*agent.Agent, 0, len(agentRegistry))
	for _, p := range agentRegistry {
		out = append(out, p)
	}
	return out
}

// GetAgent returns a registered agent by ID.
func GetAgent(id string) (*agent.Agent, bool) {
	agentMu.RLock()
	defer agentMu.RUnlock()
	a, ok := agentRegistry[id]
	return a, ok
}
