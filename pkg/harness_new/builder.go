package harness_new

import (
	"github.com/vince-0202/acgo/pkg/agent_new"
)

type BuildOptions struct {
	Agent       agent_new.Options
	Controllers []Controller
}

func Build(opts BuildOptions) (*agent_new.Agent, *Harness, error) {
	agent := agent_new.New(opts.Agent)
	harness := NewHarness(opts.Controllers...)
	if err := harness.Attach(agent); err != nil {
		return nil, nil, err
	}
	return agent, harness, nil
}
