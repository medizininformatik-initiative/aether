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
	// Global flags
	cfgFile string
	verbose bool
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
       aether pipeline start /data/torch/output

  3. Check status:
       aether pipeline status <job-id>

  4. List all jobs:
       aether job list

  5. Resume a job:
       aether pipeline continue <job-id>

Configuration:
  The CLI looks for configuration in the following order:
    1. --config flag
    2. ./aether.yaml (current directory)
    3. ~/.config/aether/aether.yaml (user config directory)

For more information:
  Documentation: https://github.com/medizininformatik-initiative/aether
  Report issues: https://github.com/medizininformatik-initiative/aether/issues`,
	Version: "1.0.0",
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
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./aether.yaml, ~/.config/aether/aether.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose logging")

	// Add version template
	rootCmd.SetVersionTemplate("Aether version {{.Version}}\n")
}
