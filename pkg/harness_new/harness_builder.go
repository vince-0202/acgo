package harness_new

import "github.com/vince-0202/acgo/pkg/agent_new"

type Controller interface {
	Load(agent *agent_new.Agent)
}

func NewHarness(controllers ...Controller) *Harness {
	res := &Harness{
		Controllers: make([]Controller, 0, len(controllers)),
	}
	for _, controller := range controllers {
		res.Controllers = append(res.Controllers, controller)
	}
	return res
}

type Harness struct {
	Controllers []Controller
}
