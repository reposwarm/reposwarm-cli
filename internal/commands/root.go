// Package commands defines all CLI commands.
package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/config"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	flagJSON     bool
	flagAgent    bool
	flagAPIUrl   string
	flagAPIToken string
	flagVerbose  bool
)

// NewRootCmd creates the root cobra command with all subcommands.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "reposwarm",
		Short: "CLI for RepoSwarm — AI-powered multi-repo architecture discovery",
		Long: fmt.Sprintf("RepoSwarm CLI v%s", version) + `
Provides command-line access to the RepoSwarm platform.
Discover repositories, trigger investigations, browse results, and manage prompts.

Get started:
  reposwarm new                    Bootstrap a new local installation
  reposwarm config init            Set up API connection
  reposwarm status                 Check connection and services
  reposwarm doctor                 Diagnose installation health
  reposwarm repos list             List tracked repositories
  reposwarm results list           Browse investigation results`,
		Version: version,
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			// Don't print hint for version, help, completion, or JSON mode
			name := cmd.Name()
			if name == "version" || name == "help" || name == "completion" || flagJSON {
				return
			}
			output.F.Finish()
		},
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			output.InitFormatter(!flagAgent)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.Flags().BoolP("version", "v", false, "Print version")
	root.PersistentFlags().BoolVar(&flagJSON, "json", false, "Output as JSON")
	root.PersistentFlags().BoolVar(&flagAgent, "for-agent", false, "Plain text output for agents/scripts")
	root.PersistentFlags().StringVar(&flagAPIUrl, "api-url", "", "API server URL (overrides config)")
	root.PersistentFlags().StringVar(&flagAPIToken, "api-token", "", "API bearer token (overrides config)")
	root.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "Show debug info")

	// Setup & diagnostics
	root.AddCommand(newNewCmd())
	root.AddCommand(newDoctorCmd(version))
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("reposwarm version %s\n", version)
		},
	})
	root.AddCommand(newStatusCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newUpgradeCmd(version))
	root.AddCommand(newShowCmd())
	root.AddCommand(newURLCmd())

	// Repos (includes discover as subcommand)
	root.AddCommand(newReposCmd())

	// Workflows (includes watch as subcommand)
	root.AddCommand(newWorkflowsCmd())
	root.AddCommand(newDashboardCmd())
	root.AddCommand(newErrorsCmd())
	root.AddCommand(newInvestigateCmd())
	root.AddCommand(newLogsCmd())
	root.AddCommand(newWorkersCmd())
	root.AddCommand(newPreflightCmd())
	root.AddCommand(newServicesCmd())
	root.AddCommand(newRestartCmd())
	root.AddCommand(newStopCmd())
	root.AddCommand(newStartCmd())

	// Results (includes diff, report as subcommands; show→sections)
	root.AddCommand(newResultsCmd())

	// Prompts
	root.AddCommand(newPromptsCmd())



	return root
}



// getClient creates an API client from config + flag overrides.
func getClient() (*api.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	url := cfg.APIUrl
	token := cfg.APIToken
	if flagAPIUrl != "" {
		url = flagAPIUrl
	}
	if flagAPIToken != "" {
		token = flagAPIToken
	}

	if url == "" {
		return nil, fmt.Errorf("no API URL configured: run 'reposwarm config init' or pass --api-url")
	}
	if token == "" {
		return nil, fmt.Errorf("no API token configured: run 'reposwarm config init' or pass --api-token")
	}

	return api.New(url, token), nil
}

// ctx returns a background context.
func ctx() context.Context {
	return context.Background()
}

// Execute runs the root command.
func Execute(version string) {
	root := NewRootCmd(version)
	if err := root.Execute(); err != nil {
		msg := err.Error()
		// Friendly arg errors (from friendlyExactArgs etc.) start with 💡
		// Print them directly to stderr without an extra ERROR prefix
		if len(msg) > 0 && msg[0] == 0xF0 { // UTF-8 start of emoji
			fmt.Fprintln(os.Stderr, msg)
		} else {
			output.F.Error(msg)
		}
		os.Exit(1)
	}
}
