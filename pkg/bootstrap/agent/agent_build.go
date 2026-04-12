package agent

import (
	"path/filepath"
	"strings"

	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/config"
	"github.com/vince-0202/acgo/pkg/llm"
	"github.com/vince-0202/acgo/pkg/runtime"
	"github.com/vince-0202/acgo/pkg/tools"
)

type AgentBuildConfig struct {
	ID       string
	UseModel string
	UseTools []agent.Tool
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

func BuildAgent(settings *config.Settings, options ...AgentBuilderOption) (*agent.Agent, error) {
	builder := buildAgentConfig(settings, options...)
	ag := newAgent(settings, builder)
	if err := runtime.RegisterAgent(ag); err != nil {
		return nil, err
	}
	return ag, nil
}

func buildAgentConfig(settings *config.Settings, options ...AgentBuilderOption) AgentBuildConfig {
	builder := AgentBuildConfig{
		UseModel: settings.Agent.DefaultModel,
		UseTools: []agent.Tool{},
	}
	for _, option := range options {
		if option != nil {
			option(&builder)
		}
	}
	return builder
}

func newAgent(settings *config.Settings, builder AgentBuildConfig) *agent.Agent {
	provider, model := loadProviderAndModel(builder.UseModel)
	workDir := filepath.Join(settings.WorkDir, builder.ID)
	return agent.New(agent.Options{
		ID:       builder.ID,
		WorkDir:  workDir,
		Provider: provider,
		Model:    model,
		Tools:    builder.UseTools,
		InitialState: agent.State{
			WorkDir: workDir,
		},
	})
}

func loadProviderAndModel(modelID string) (llm.Provider, llm.Model) {
	if got, ok := runtime.GetModel(modelID); ok {
		provider, _ := runtime.GetProvider(got.Provider)
		return provider, got
	}
	return nil, llm.Model{}
}
