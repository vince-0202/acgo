package harness

import (
	"github.com/vince-0202/acgo/pkg/config"
	baseharness "github.com/vince-0202/acgo/pkg/harness"
)

type BuildConfig struct {
	MemoryWriter             baseharness.MemoryWriter
	Monitoring               baseharness.MonitoringOptions
	PermissionMode           baseharness.PermissionMode
	PermissionRules          []baseharness.PermissionRule
	PermissionConfirmHook    baseharness.PermissionConfirmHook
	ChildAgentBuilder        baseharness.ChildAgentBuilder
	ChildControllerFactories []baseharness.ChildControllerFactory
	RegisterSubAgentTool     *bool
}

type BuilderOption func(*BuildConfig)

func WithMemory(mem baseharness.MemoryWriter) BuilderOption {
	return func(config *BuildConfig) {
		config.MemoryWriter = mem
	}
}

func WithMonitoring(opts baseharness.MonitoringOptions) BuilderOption {
	return func(config *BuildConfig) {
		config.Monitoring = opts
	}
}

func WithPermissionMode(mode baseharness.PermissionMode) BuilderOption {
	return func(config *BuildConfig) {
		config.PermissionMode = mode
	}
}

func WithPermissionRules(rules ...baseharness.PermissionRule) BuilderOption {
	return func(config *BuildConfig) {
		config.PermissionRules = append(config.PermissionRules, rules...)
	}
}

func WithPermissionConfirmHook(hook baseharness.PermissionConfirmHook) BuilderOption {
	return func(config *BuildConfig) {
		config.PermissionConfirmHook = hook
	}
}

func WithChildAgentBuilder(builder baseharness.ChildAgentBuilder) BuilderOption {
	return func(config *BuildConfig) {
		config.ChildAgentBuilder = builder
	}
}

func WithChildControllerFactories(factories ...baseharness.ChildControllerFactory) BuilderOption {
	return func(config *BuildConfig) {
		config.ChildControllerFactories = append(config.ChildControllerFactories, factories...)
	}
}

func WithRegisterSubAgentTool(enabled bool) BuilderOption {
	return func(config *BuildConfig) {
		config.RegisterSubAgentTool = &enabled
	}
}

func BuildDefaultHarness(options ...BuilderOption) *baseharness.Harness {
	config := BuildConfig{
		PermissionMode: baseharness.PermissionModeDefault,
	}
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}

	return baseharness.BuildDefaultHarness(baseharness.DefaultHarnessOptions{
		MemoryWriter:             config.MemoryWriter,
		Monitoring:               config.Monitoring,
		PermissionMode:           config.PermissionMode,
		PermissionRules:          config.PermissionRules,
		PermissionConfirmHook:    config.PermissionConfirmHook,
		ChildAgentBuilder:        config.ChildAgentBuilder,
		ChildControllerFactories: config.ChildControllerFactories,
		RegisterSubAgentTool:     config.RegisterSubAgentTool,
	})
}

func BuildDefaultHarnessBySettings(settings *config.Settings, options ...BuilderOption) *baseharness.Harness {
	if settings != nil {
		options = append([]BuilderOption{
			WithMonitoring(baseharness.MonitoringOptions{
				MetricsFilePath: settings.Monitoring.MetricsFilePath,
				LLMFilePath:     settings.Monitoring.LLMFilePath,
			}),
		}, options...)
	}
	return BuildDefaultHarness(options...)
}
