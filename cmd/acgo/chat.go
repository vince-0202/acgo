package main

import (
	"acgo/pkg/tui"

	"github.com/spf13/cobra"
)

// chatCmd runs the interactive TUI; optional --session to load an existing session.
var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start the interactive coding agent TUI",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionPath, _ := cmd.Flags().GetString("session")
		return tui.Run(sessionPath)
	},
}

func init() {
	chatCmd.Flags().String("session", "", "Path to session file to load (default: create new under ~/.acgo/sessions)")
	rootCmd.AddCommand(chatCmd)
}
