package commands

import (
	"fmt"

	"github.com/reposwarm/reposwarm-cli/internal/api"
	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newDiscoverCmd() *cobra.Command {
	var sourceFlag string
	var orgFlag string

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Auto-discover repositories from your configured git provider",
		Long:  "Triggers server-side discovery of repositories from your configured git provider and adds new ones to tracking.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			// Auto-detect source from config if not specified
			source := sourceFlag
			if source == "" {
				cfg, cfgErr := config.Load()
				if cfgErr == nil && cfg.GitProvider != "" {
					source = cfg.GitProvider
				} else {
					source = "codecommit"
				}
			}

			body := map[string]any{
				"source": source,
			}
			if orgFlag != "" {
				body["org"] = orgFlag
			}

			var result api.DiscoverResult
			if err := client.Post(ctx(), "/repos/discover", body, &result); err != nil {
				return err
			}

			if flagJSON {
				return output.JSON(result)
			}

			F := output.F
			F.Success(fmt.Sprintf("Discovered %d repos from %s", result.Discovered, source))
			if result.Added > 0 {
				F.Success(fmt.Sprintf("Added %d new repos", result.Added))
			} else {
				F.Info(fmt.Sprintf("All repos already tracked (%d skipped)", result.Skipped))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&sourceFlag, "source", "", "Git provider source (github, codecommit, gitlab, azure, bitbucket). Defaults to configured provider.")
	cmd.Flags().StringVar(&orgFlag, "org", "", "GitHub organization name (optional, for GitHub org repos)")

	return cmd
}
