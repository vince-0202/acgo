package main

import (
	"github.com/spf13/cobra"
	"github.com/vince-0202/acgo/pkg/tui_new"
)

var chatNewSessionID string

var chatNewCmd = &cobra.Command{
	Use:   "chat-new",
	Short: "Start the new interactive TUI (agent_new runtime)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui_new.Run(chatNewSessionID)
	},
}

func init() {
	chatNewCmd.Flags().StringVarP(&chatNewSessionID, "session", "s", "", "Session id to load (default: create new)")
	rootCmd.AddCommand(chatNewCmd)
}
