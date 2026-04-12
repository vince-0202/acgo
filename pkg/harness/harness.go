package harness

import (
	"fmt"

	"github.com/vince-0202/acgo/pkg/agent"
)

type Controller interface {
	Name() string
	Install(agent agent.AgentRuntime) (func(), error)
}

type Harness struct {
	Controllers []Controller
	uninstalls  []func()
}

func NewHarness(controllers ...Controller) *Harness {
	res := &Harness{
		Controllers: make([]Controller, 0, len(controllers)),
		uninstalls:  make([]func(), 0, len(controllers)),
	}
	res.Controllers = append(res.Controllers, controllers...)
	return res
}

func (h *Harness) Attach(agent agent.AgentRuntime) error {
	if h == nil {
		return nil
	}
	for _, controller := range h.Controllers {
		if controller == nil {
			continue
		}
		uninstall, err := controller.Install(agent)
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
}
