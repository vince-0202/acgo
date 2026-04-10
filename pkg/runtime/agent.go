package runtime

import (
	"errors"
	"github.com/vince-0202/acgo/pkg/agent"
	"strings"
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

// FindSubAgentsByRole returns parent-subID pairs whose profile role matches.
func FindSubAgentsByRole(role string) map[string][]string {
	role = strings.TrimSpace(strings.ToLower(role))
	out := make(map[string][]string)
	if role == "" {
		return out
	}
	agentMu.RLock()
	defer agentMu.RUnlock()
	for agentID, ag := range agentRegistry {
		sac := ag.SubAgentController()
		if sac == nil {
			continue
		}
		list := sac.List()
		for _, info := range list {
			if strings.ToLower(strings.TrimSpace(info.Role)) == role {
				out[agentID] = append(out[agentID], info.SubID)
			}
		}
	}
	return out
}

// FindSubAgentsByCapability returns parent-subID pairs whose profile capabilities include capability.
func FindSubAgentsByCapability(capability string) map[string][]string {
	capability = strings.TrimSpace(strings.ToLower(capability))
	out := make(map[string][]string)
	if capability == "" {
		return out
	}
	agentMu.RLock()
	defer agentMu.RUnlock()
	for agentID, ag := range agentRegistry {
		sac := ag.SubAgentController()
		if sac == nil {
			continue
		}
		for _, info := range sac.List() {
			prof, ok := sac.GetProfile(info.SubID)
			if !ok {
				continue
			}
			for _, cap := range prof.Capabilities {
				if strings.ToLower(strings.TrimSpace(cap)) == capability {
					out[agentID] = append(out[agentID], info.SubID)
					break
				}
			}
		}
	}
	return out
}
