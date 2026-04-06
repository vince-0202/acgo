package main

import (
	"fmt"
	"github.com/vince-0202/acgo/pkg/bootstrap/setting"
	"github.com/vince-0202/acgo/pkg/session"

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
		settings, err := setting.LoadAndRuntimeInit()
		if err != nil {
			return err
		}
		paths, err := session.List(settings.Session.Root)
		if err != nil {
			return err
		}
		for _, p := range paths {
			fmt.Println(p)
		}
		return nil
	},
}

func init() {
	sessionCmd.AddCommand(sessionListCmd)
	rootCmd.AddCommand(sessionCmd)
}
