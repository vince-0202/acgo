package main

import (
	"context"
	"fmt"
	"github.com/vince-0202/acgo/pkg/agent"
	"github.com/vince-0202/acgo/pkg/bootstrap"
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
	settings, err := bootstrap.LoadAndRuntimeInit(
		config.WithDirectName(".acgo"),
		config.WithName("settings"),
	)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	//build agent with settings and other options
	ag := bootstrap.BuildAgent(
		settings,
		bootstrap.WithId("example-bootstrap-agent"),
		bootstrap.WithDefaultTools(),
	)
	if ag == nil {
		return fmt.Errorf("build agent failed")
	}

	unsub := ag.Subscribe(func(e agent.Event) {
		switch e.Type {
		case agent.EventMessageEnd:
			fmt.Printf("%s: %s\n", e.Message.Role, e.Message.Content)
		}
	})
	defer unsub()

	userText := "你好，请用一句话介绍你自己。"
	if err := ag.Prompt(context.Background(), userText); err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}

	return nil
}
