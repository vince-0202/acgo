package agent

import (
	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/harness"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/vince-0202/acgo/pkg/log"
	"github.com/vince-0202/acgo/pkg/runtime"
	"github.com/vince-0202/acgo/pkg/tools"
	"path/filepath"
)

type AgentBuildConfig struct {
	Id       string
	UseModel string
	UseTools []harness.Tool
	// UseMemory overrides the default from harness.MemoryController.Load (memory.DefaultManager).
	UseMemory harness.MemoryWriter
}

func BuildAgent(settings *config.Settings, options ...AgentBuilderOption) *agent.Agent {
	builder := AgentBuildConfig{
		UseModel:  settings.Agent.DefaultModel,
		UseTools:  []harness.Tool{},
		UseMemory: nil,
	}
	for _, option := range options {
		option(&builder)
	}

	provider, module := loadProviderAndModule(builder)

	ag := agent.New(builder.Id, agent.Options{
		WorkDir:  filepath.Join(settings.WorkDir, builder.Id),
		Provider: provider,
		Model:    module,
		UseTools: builder.UseTools,
		InitialState: agent.State{
			WorkDir: filepath.Join(settings.WorkDir, builder.Id),
		},
		MemoryWriter: builder.UseMemory,
	})

	if err := runtime.RegisterAgent(ag); err != nil {
		log.Debugf("Failed to register agent: %v", err)
		return nil
	}
	return ag
}

func loadProviderAndModule(builderConfig AgentBuildConfig) (llm.Provider, llm.Model) {
	if modeGot, ok := runtime.GetModel(builderConfig.UseModel); ok {
		provider, _ := runtime.GetProvider(modeGot.Provider)
		return provider, modeGot
	}
	return nil, llm.Model{}
}

type AgentBuilderOption func(*AgentBuildConfig)

func WithModel(model string) AgentBuilderOption {
	return func(config *AgentBuildConfig) {
		config.UseModel = model
	}
}
func WithTools(tools ...harness.Tool) AgentBuilderOption {
	return func(config *AgentBuildConfig) {
		config.UseTools = append(config.UseTools, tools...)
	}
}
func WithId(id string) AgentBuilderOption {
	return func(config *AgentBuildConfig) {
		config.Id = id
	}
}

func WithDefaultTools() AgentBuilderOption {
	builtinTools := []harness.Tool{
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
	}
	return func(config *AgentBuildConfig) {
		config.UseTools = append(config.UseTools, builtinTools...)
	}
}

func WithMemory(mem harness.MemoryWriter) AgentBuilderOption {
	return func(config *AgentBuildConfig) {
		config.UseMemory = mem
	}
}
