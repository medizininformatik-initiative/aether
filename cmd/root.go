/*
Copyright © 2025 Aether Contributors

Aether is a CLI tool for orchestrating Data Use Process (DUP) pipelines.
*/
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// verbose toggles debug-level logging across all subcommands.
	verbose bool

	// version is set via SetVersion before Execute
	version = "dev"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "aether",
	Short: "Aether - Data Use Process (DUP) Pipeline CLI",
	Long: `Aether orchestrates Data Use Process (DUP) pipelines for medical FHIR data.

The CLI imports TORCH-extracted FHIR data and processes it through configurable steps:
  • DIMP pseudonymization (de-identification)
  • Validation (placeholder - not yet implemented)
  • Format conversion (CSV/Parquet - services not available yet)

Key Features:
  • Session-independent: Resume pipelines across terminal sessions
  • Hybrid retry: Automatic for transient errors, manual for validation failures
  • Real-time progress: Progress bars with ETA, throughput, and completion %
  • File-based state: All job data persisted to filesystem

Quick Start:
  1. Create configuration:
       cp config/aether.example.yaml aether.yaml

  2. Start a pipeline:
       aether pipeline start aether.yaml crtdl.json

  3. Check status:
       aether pipeline status aether.yaml <job-id>

  4. List all jobs:
       aether job list aether.yaml

  5. Resume a job:
       aether pipeline continue aether.yaml <job-id>

Configuration:
  Every command that needs configuration takes the path to your
  aether.yaml as the first positional argument. There is no
  --config flag and no implicit discovery of ./aether.yaml or
  ~/.config/aether/aether.yaml — point at the file explicitly.

For more information:
  Documentation: https://github.com/medizininformatik-initiative/aether
  Report issues: https://github.com/medizininformatik-initiative/aether/issues`,
	Version: version,
}

// SetVersion sets the version displayed by --version. Call before Execute.
func SetVersion(v string) {
	version = v
	rootCmd.Version = v
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	// Persistent flags (available to all subcommands)
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose logging")

	// Add version template
	rootCmd.SetVersionTemplate("Aether version {{.Version}}\n")
}
