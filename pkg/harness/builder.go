package harness

import (
	"github.com/vince-0202/acgo/pkg/agent"
)

type BuildOptions struct {
	Agent       agent.Options
	Controllers []Controller
}

func Build(opts BuildOptions) (*agent.Agent, *Harness, error) {
	agent := agent.New(opts.Agent)
	harness := NewHarness(opts.Controllers...)
	if err := harness.Attach(agent); err != nil {
		return nil, nil, err
	}
	return agent, harness, nil
}
