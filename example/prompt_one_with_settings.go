package main

import (
	"context"
	"fmt"
	"github.com/vince-0202/acgo/pkg/bootstrap/agent"
	"github.com/vince-0202/acgo/pkg/bootstrap/setting"
	"github.com/vince-0202/acgo/pkg/communi"
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
	ag := agent.BuildAgent(
		settings,
		agent.WithId("example-bootstrap-agent"),
		agent.WithDefaultTools(),
	)
	if ag == nil {
		return fmt.Errorf("build agent failed")
	}

	unsub := ag.Subscribe(func(e communi.AgentEvent) {
		switch e.Type {
		case communi.EventMessageEnd:
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
