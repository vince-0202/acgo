package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

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
	rootCmd.AddCommand(printCmd)
}
