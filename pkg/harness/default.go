package harness

type DefaultHarnessOptions struct {
	MemoryWriter             MemoryWriter
	MemoryWriters            []MemoryWriter
	Monitoring               MonitoringOptions
	PermissionMode           PermissionMode
	PermissionRules          []PermissionRule
	PermissionConfirmHook    PermissionConfirmHook
	ChildAgentBuilder        ChildAgentBuilder
	ChildControllerFactories []ChildControllerFactory
	RegisterSubAgentTool     *bool
}

func BuildDefaultHarness(opts DefaultHarnessOptions) *Harness {
	registerSubAgentTool := true
	if opts.RegisterSubAgentTool != nil {
		registerSubAgentTool = *opts.RegisterSubAgentTool
	}
	memoryWriters := append([]MemoryWriter(nil), opts.MemoryWriters...)
	if opts.MemoryWriter != nil {
		memoryWriters = append(memoryWriters, opts.MemoryWriter)
	}

	permission := NewPermissionController(opts.PermissionMode, opts.PermissionRules, opts.PermissionConfirmHook)
	factories := opts.ChildControllerFactories
	if len(factories) == 0 {
		factories = []ChildControllerFactory{
			func() Controller { return NewContextController() },
			func() Controller { return NewSkillsController() },
			func() Controller { return permission.Clone() },
			func() Controller { return NewMemoryController(memoryWriters...) },
			func() Controller { return NewMonitoringController(opts.Monitoring) },
		}
	}

	return NewHarness(
		NewContextController(),
		NewSkillsController(),
		permission,
		NewMemoryController(memoryWriters...),
		NewMonitoringController(opts.Monitoring),
		NewSubAgentController(SubAgentControllerOptions{
			Builder:                  opts.ChildAgentBuilder,
			ChildControllerFactories: factories,
			RegisterTool:             registerSubAgentTool,
		}),
	)
}
