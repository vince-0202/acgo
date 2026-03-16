package main

import (
	"fmt"
	"os"

	"acgo/pkg/config"
	"acgo/pkg/log"
	"acgo/pkg/session"
	"acgo/pkg/tui"

	"github.com/spf13/cobra"
)

// rootCmd is the entrypoint for the acgo CLI.
var rootCmd = &cobra.Command{
	Use:   "acgo",
	Short: "acgo is a Go-based coding agent CLI",
	Long:  "acgo is a Go-based coding agent CLI ",
}

// chatCmd runs the interactive TUI; optional --session to load an existing session.
var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start the interactive coding agent TUI",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionPath, _ := cmd.Flags().GetString("session")
		return tui.Run(sessionPath)
	},
}

// printCmd will provide a simple one-shot request/response interface.
var printCmd = &cobra.Command{
	Use:   "print [prompt]",
	Short: "Send a single prompt and print the response",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Placeholder implementation for now.
		prompt := args[0]
		fmt.Printf("acgo print: got prompt %q (LLM not wired yet)\n", prompt)
		return nil
	},
}

func init() {
	chatCmd.Flags().String("session", "", "Path to session file to load (default: create new under ~/.acgo/sessions)")
	rootCmd.AddCommand(chatCmd)
	rootCmd.AddCommand(printCmd)
	rootCmd.AddCommand(sessionCmd)
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionShowCmd)
}

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

func main() {
	// Single log config for the whole project: load config and init log once before any command.
	if settings, err := config.LoadSettings(); err == nil {
		log.InitWithFile(settings.Log.Level, settings.Log.FilePath)
	}
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
