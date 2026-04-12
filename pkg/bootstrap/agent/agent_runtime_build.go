package agent

import (
	"path/filepath"

	"github.com/vince-0202/acgo/pkg/agent_new"
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/harness"
	"github.com/vince-0202/acgo/pkg/harness_new"
)

func BuildAgentRuntime(settings *config.Settings, options ...AgentBuilderOption) (*agent_new.Agent, *harness_new.Harness, error) {
	builder := AgentBuildConfig{
		UseModel:  settings.Agent.DefaultModel,
		UseTools:  []harness.Tool{},
		UseMemory: nil,
	}
	for _, option := range options {
		option(&builder)
	}

	provider, model := loadProviderAndModule(builder)
	permission := harness_new.NewPermissionController(harness_new.PermissionModeDefault, nil, nil)

	return harness_new.Build(harness_new.BuildOptions{
		Agent: agent_new.Options{
			ID:       builder.Id,
			WorkDir:  filepath.Join(settings.WorkDir, builder.Id),
			Provider: provider,
			Model:    model,
			Tools:    harness_new.AdaptLegacyTools(builder.UseTools...),
			InitialState: agent_new.State{
				WorkDir: filepath.Join(settings.WorkDir, builder.Id),
			},
		},
		Controllers: []harness_new.Controller{
			harness_new.NewContextController(),
			permission,
			harness_new.NewSubAgentController(harness_new.SubAgentControllerOptions{
				ChildControllerFactories: []harness_new.ChildControllerFactory{
					func() harness_new.Controller { return harness_new.NewContextController() },
					func() harness_new.Controller { return permission.Clone() },
				},
			}),
		},
	})
}
