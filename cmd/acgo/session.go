package main

import (
	"fmt"
	"os"

	"acgo/pkg/config"
	"acgo/pkg/session"

	"github.com/spf13/cobra"
)

// sessionCmd provides session list/show (stub for later).
var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage chat sessions",
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List session files",
	RunE: func(cmd *cobra.Command, args []string) error {
		settings, err := config.LoadSettings()
		if err != nil {
			return err
		}
		root := settings.Session.Root
		if root == "" {
			home, _ := os.UserHomeDir()
			root = home + "/.acgo/sessions"
		}
		paths, err := session.List(root)
		if err != nil {
			return err
		}
		for _, p := range paths {
			fmt.Println(p)
		}
		return nil
	},
}

var sessionShowCmd = &cobra.Command{
	Use:   "show [path]",
	Short: "Show session path or list (when no path: show sessions root)",
	RunE: func(cmd *cobra.Command, args []string) error {
		settings, err := config.LoadSettings()
		if err != nil {
			return err
		}
		root := settings.Session.Root
		if root == "" {
			home, _ := os.UserHomeDir()
			root = home + "/.acgo/sessions"
		}
		if len(args) == 0 {
			fmt.Println("Sessions root:", root)
			return nil
		}
		fmt.Println(args[0])
		return nil
	},
}

func init() {
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionShowCmd)
	rootCmd.AddCommand(sessionCmd)
}
