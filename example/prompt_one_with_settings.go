package main

import (
	"context"
	"fmt"
	"github.com/vince-0202/acgo/pkg/agent"
	bootstrapagent "github.com/vince-0202/acgo/pkg/bootstrap/agent"
	bootstrapharness "github.com/vince-0202/acgo/pkg/bootstrap/harness"
	"github.com/vince-0202/acgo/pkg/bootstrap/setting"
	"github.com/vince-0202/acgo/pkg/config"
)

func main() {
	if err := PromptOne(); err != nil {
		return
	}
}

// PromptOne demonstrates the minimal bootstrap workflow:
// load settings, build an agent, run one prompt, and print the reply.
func PromptOne() error {
	//load config from ${HOME}/.acgo/settings.yaml
	settings, err := setting.LoadAndRuntimeInit(
		config.WithDirectName(".acgo"),
		config.WithName("settings"),
	)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	//build agent with settings and other options
	ag, err := bootstrapagent.BuildAgent(
		settings,
		bootstrapagent.WithId("example-bootstrap-agent"),
		bootstrapagent.WithDefaultTools(),
	)
	if err != nil {
		return fmt.Errorf("build agent failed: %w", err)
	}
	h := bootstrapharness.BuildDefaultHarness()
	if err := h.Attach(ag); err != nil {
		return fmt.Errorf("attach harness failed: %w", err)
	}

	unsub := h.Subscribe(func(e agent.Event, abort func()) {
		switch e.Type {
		case agent.EventMessageEnd:
			if e.Message == nil {
				return
			}
			fmt.Printf("%s: %s\n", e.Message.Role, e.Message.ContentBlocksToText())
		}
	})
	defer unsub()

	userText := "你好，请用一句话介绍你自己。"
	if err := h.Prompt(context.Background(), userText); err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}

	return nil
}
