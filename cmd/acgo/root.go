package main

import (
	"acgo/pkg/config"
	"acgo/pkg/log"
	"github.com/spf13/cobra"
)

// rootCmd is the entrypoint for the acgo CLI.
var rootCmd = &cobra.Command{
	Use:   "acgo",
	Short: "acgo is a Go-based coding agent CLI",
	Long:  "acgo is a Go-based coding agent CLI ",
}

func init() {
	// Single log config for the whole project: load config and init log once before any command.
	if settings, err := config.LoadSettings(); err == nil {
		log.InitWithFile(settings.Log.Level, settings.Log.FilePath)
	}
}
