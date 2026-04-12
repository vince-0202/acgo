package harness

type DefaultHarnessOptions struct {
	MemoryWriter             MemoryWriter
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
	permission := NewPermissionController(opts.PermissionMode, opts.PermissionRules, opts.PermissionConfirmHook)
	factories := opts.ChildControllerFactories
	if len(factories) == 0 {
		factories = []ChildControllerFactory{
			func() Controller { return NewContextController() },
			func() Controller { return NewSkillsController() },
			func() Controller { return permission.Clone() },
			func() Controller { return NewMemoryController(opts.MemoryWriter) },
		}
	}

	return NewHarness(
		NewContextController(),
		NewSkillsController(),
		permission,
		NewMemoryController(opts.MemoryWriter),
		NewSubAgentController(SubAgentControllerOptions{
			Builder:                  opts.ChildAgentBuilder,
			ChildControllerFactories: factories,
			RegisterTool:             registerSubAgentTool,
		}),
	)
}
