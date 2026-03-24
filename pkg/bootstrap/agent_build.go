package bootstrap

import (
	"acgo/pkg/agent"
	"acgo/pkg/config"
	"acgo/pkg/llm"
	"acgo/pkg/log"
	"acgo/pkg/memory"
	"acgo/pkg/runtime"
	"acgo/pkg/tools"
	"path/filepath"
)

type AgentBuildConfig struct {
	Id        string
	UseModel  string
	UseTools  []agent.AgentTool
	UseMemory agent.MemoryWriter
}

func BuildAgent(settings *config.Settings, options ...AgentBuilderOption) *agent.Agent {
	builder := AgentBuildConfig{
		UseModel:  settings.Agent.DefaultModel,
		UseTools:  []agent.AgentTool{},
		UseMemory: nil,
	}
	for _, option := range options {
		option(&builder)
	}

	if builder.UseMemory == nil {
		mgr, err := memory.DefaultManager()
		if err == nil {
			builder.UseMemory = mgr
		}
	}

	ag := agent.New(builder.Id, agent.Options{
		WorkDir:  filepath.Join(settings.WorkDir, builder.Id),
		StreamFn: runtime.DefaultStreamFn,
		InitialState: agent.State{
			Model: loadModule(builder),
			Tools: builder.UseTools,
		},
		MemoryWriter: builder.UseMemory,
	})

	if err := runtime.RegisterAgent(ag); err != nil {
		log.Debugf("Failed to register agent: %v", err)
		return nil
	}
	return ag
}

func loadModule(builderConfig AgentBuildConfig) llm.Model {
	var m llm.Model
	if got, ok := runtime.GetModel(builderConfig.UseModel); ok {
		m = got
	}
	return m
}

type AgentBuilderOption func(*AgentBuildConfig)

func WithModel(model string) AgentBuilderOption {
	return func(config *AgentBuildConfig) {
		config.UseModel = model
	}
}
func WithTools(tools ...agent.AgentTool) AgentBuilderOption {
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
	builtinTools := []agent.AgentTool{
		tools.NewReadTool(),
		tools.NewWriteTool(),
		tools.NewBashTool(),
		tools.NewEditTool(),
		tools.NewGrepTool(),
		tools.NewListTool(),
		tools.NewRagTool(),
		tools.NewMemoryRecallTool(),
		tools.NewSkillSearchTool(),
	}
	return func(config *AgentBuildConfig) {
		config.UseTools = append(config.UseTools, builtinTools...)
	}
}

func WithMemory(mem agent.MemoryWriter) AgentBuilderOption {
	return func(config *AgentBuildConfig) {
		config.UseMemory = mem
	}
}
