package main

import (
	tui "github.com/vince-0202/acgo/pkg/tui"

	"github.com/spf13/cobra"
)

var (
	sessionId string
)

// chatCmd runs the interactive TUI; optional --session to load an existing session.
var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start the interactive coding agent TUI",
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.Run(sessionId)
	},
}

func init() {
	chatCmd.Flags().StringVarP(&sessionId, "session", "s", "", "Path to session file to load (default: create new under ~/.acgo/sessions)")
	rootCmd.AddCommand(chatCmd)
}
