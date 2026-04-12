package agent

import (
	"path/filepath"
	"strings"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/harness"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/vince-0202/acgo/pkg/runtime"
	"github.com/vince-0202/acgo/pkg/tools"
)

type AgentBuildConfig struct {
	ID        string
	UseModel  string
	UseTools  []agent.Tool
	UseMemory harness.MemoryWriter
}

type AgentBuilderOption func(*AgentBuildConfig)

func WithModel(model string) AgentBuilderOption {
	return func(config *AgentBuildConfig) {
		config.UseModel = model
	}
}

func WithTools(toolList ...agent.Tool) AgentBuilderOption {
	return func(config *AgentBuildConfig) {
		config.UseTools = append(config.UseTools, toolList...)
	}
}

func WithID(id string) AgentBuilderOption {
	return func(config *AgentBuildConfig) {
		config.ID = id
	}
}

func WithMemory(mem harness.MemoryWriter) AgentBuilderOption {
	return func(config *AgentBuildConfig) {
		config.UseMemory = mem
	}
}

func WithId(id string) AgentBuilderOption {
	return WithID(id)
}

func WithDefaultTools() AgentBuilderOption {
	builtinTools := []agent.Tool{
		tools.NewReadTool(),
		tools.NewWriteTool(),
		tools.NewBashTool(),
		tools.NewEditTool(),
		tools.NewGrepTool(),
		tools.NewListTool(),
		tools.NewWebFetchTool(),
		tools.NewWebSearchTool(),
		tools.NewCronCreateTool(),
		tools.NewCronDeleteTool(),
		tools.NewCronListTool(),
		tools.NewToolFlowTool(),
		tools.NewRagTool(),
		tools.NewMemoryRecallTool(),
		tools.NewGitStatusTool(),
		tools.NewGitDiffTool(),
		tools.NewGitLogTool(),
		tools.NewGitBranchTool(),
		tools.NewGitAddTool(),
		tools.NewGitCommitTool(),
		tools.NewGitWorktreeTool(),
	}
	return func(config *AgentBuildConfig) {
		config.UseTools = append(config.UseTools, builtinTools...)
	}
}

func WithDefaultToolsExcept(excluded ...string) AgentBuilderOption {
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, name := range excluded {
		n := strings.TrimSpace(strings.ToLower(name))
		if n == "" {
			continue
		}
		excludedSet[n] = struct{}{}
	}
	return func(config *AgentBuildConfig) {
		collector := AgentBuildConfig{}
		WithDefaultTools()(&collector)
		for _, tool := range collector.UseTools {
			if tool == nil {
				continue
			}
			if _, skip := excludedSet[strings.TrimSpace(strings.ToLower(tool.Name()))]; skip {
				continue
			}
			config.UseTools = append(config.UseTools, tool)
		}
	}
}

func BuildAgentRuntime(settings *config.Settings, options ...AgentBuilderOption) (*agent.Agent, *harness.Harness, error) {
	builder := AgentBuildConfig{
		UseModel:  settings.Agent.DefaultModel,
		UseTools:  []agent.Tool{},
		UseMemory: nil,
	}
	for _, option := range options {
		option(&builder)
	}

	provider, model := loadProviderAndModel(builder.UseModel)
	permission := harness.NewPermissionController(harness.PermissionModeDefault, nil, nil)

	ag, h, err := harness.Build(harness.BuildOptions{
		Agent: agent.Options{
			ID:       builder.ID,
			WorkDir:  filepath.Join(settings.WorkDir, builder.ID),
			Provider: provider,
			Model:    model,
			Tools:    builder.UseTools,
			InitialState: agent.State{
				WorkDir: filepath.Join(settings.WorkDir, builder.ID),
			},
		},
		Controllers: []harness.Controller{
			harness.NewContextController(),
			harness.NewSkillsController(),
			permission,
			harness.NewMemoryController(builder.UseMemory),
			harness.NewSubAgentController(harness.SubAgentControllerOptions{
				ChildControllerFactories: []harness.ChildControllerFactory{
					func() harness.Controller { return harness.NewContextController() },
					func() harness.Controller { return harness.NewSkillsController() },
					func() harness.Controller { return permission.Clone() },
					func() harness.Controller { return harness.NewMemoryController(builder.UseMemory) },
				},
			}),
		},
	})
	if err != nil {
		return nil, nil, err
	}
	if err := runtime.RegisterAgent(ag); err != nil {
		return nil, nil, err
	}
	return ag, h, nil
}

func loadProviderAndModel(modelID string) (llm.Provider, llm.Model) {
	if got, ok := runtime.GetModel(modelID); ok {
		provider, _ := runtime.GetProvider(got.Provider)
		return provider, got
	}
	return nil, llm.Model{}
}
