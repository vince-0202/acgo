package main

import (
	"fmt"
	"os"

	"acgo/pkg/config"
	"acgo/pkg/log"
	"acgo/pkg/tui"

	"github.com/spf13/cobra"
)

// rootCmd is the entrypoint for the acgo CLI.
var rootCmd = &cobra.Command{
	Use:   "acgo",
	Short: "acgo is a Go-based coding agent CLI",
	Long:  "acgo is a Go-based coding agent CLI ",
}

// chatCmd will host the interactive TUI in later stages.
var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start the interactive coding agent TUI",
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.Run()
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
	rootCmd.AddCommand(chatCmd)
	rootCmd.AddCommand(printCmd)
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
