package agent_new

import (
	"github.com/vince-0202/acgo/pkg/communi"
	"github.com/vince-0202/acgo/pkg/harness"
	"sync"
)

type SubAgentManager struct {
	parent   *Agent
	mu       sync.Mutex
	children map[string]*Agent // logical subID -> child agent
	profiles map[string]harness.SubAgentProfile
	bus      *communi.MessageBus
}
