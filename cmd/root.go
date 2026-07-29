package cmd

import (
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"
)

// migrationsFS is the embedded migrations filesystem, injected from main.
var migrationsFS fs.FS

var rootCmd = &cobra.Command{
	Use:   "passwordless-auth-service",
	Short: "Passwordless phone-signup and session service",
}

// Execute runs the root command with the embedded migrations filesystem.
func Execute(migrations fs.FS) {
	migrationsFS = migrations
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
