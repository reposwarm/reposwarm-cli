package commands

import (
	"fmt"
	"strings"

	"github.com/reposwarm/reposwarm-cli/internal/api"
	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newInvestigateCmd() *cobra.Command {
	var model string
	var chunkSize, parallel int
	var all, force, replace, dryRun bool

	cmd := &cobra.Command{
		Use:   "investigate [repo]",
		Short: "Trigger architecture investigation",
		Long: `Trigger an AI-powered architecture investigation for one or all repos.

Examples:
  reposwarm investigate is-odd              # Single repo
  reposwarm investigate --all               # All enabled repos
  reposwarm investigate is-odd --model us.anthropic.claude-opus-4-6`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			cfg, _ := config.Load()
			if model == "" {
				model = cfg.DefaultModel
			}
			if chunkSize == 0 {
				chunkSize = cfg.ChunkSize
			}

			if len(args) > 0 {
				repoArg := args[0]

				// --replace: terminate existing workflow for this repo
				if replace {
					var wfResult api.WorkflowsResponse
					if err := client.Get(ctx(), "/workflows?pageSize=50", &wfResult); err == nil {
						for _, w := range wfResult.Executions {
							if (w.Status == "RUNNING" || w.Status == "Running") && repoName(w.WorkflowID) == repoArg {
								if !flagJSON {
									output.F.Warning(fmt.Sprintf("Terminating existing workflow: %s", w.WorkflowID))
								}
								body := map[string]string{"reason": "Replaced via investigate --replace"}
								var termResult any
								_ = client.Post(ctx(), "/workflows/"+w.WorkflowID+"/terminate", body, &termResult)
							}
						}
					}
				}

				// Pre-flight checks (unless --force)
				if !force {
					checks := runPreflightChecks(repoArg)
					failed := 0
					for _, c := range checks {
						if c.Status == "fail" {
							failed++
						}
					}
					if failed > 0 {
						if flagJSON {
							return output.JSON(map[string]any{
								"error":   "pre-flight failed",
								"checks":  checks,
								"hint":    "Use --force to skip pre-flight checks",
							})
						}
						output.F.Println()
						output.F.Error("Cannot start investigation: pre-flight checks failed")
						output.F.Println()
						for _, c := range checks {
							switch c.Status {
							case "ok":
								output.F.Printf("  %s %s: %s\n", output.Green("[OK]"), c.Name, c.Message)
							case "fail":
								output.F.Printf("  %s %s: %s\n", output.Red("[FAIL]"), c.Name, c.Message)
							case "warn":
								output.F.Printf("  %s %s: %s\n", output.Yellow("[WARN]"), c.Name, c.Message)
							}
						}
						output.F.Println()
						output.F.Info("Run: reposwarm doctor for full diagnostics")
						output.F.Info("Use: reposwarm investigate " + repoArg + " --force to start anyway")
						return fmt.Errorf("pre-flight failed (%d issues)", failed)
					}

					if !flagJSON && !dryRun {
						// Show passing pre-flight summary
						healthy := 0
						for _, c := range checks {
							if c.Status == "ok" {
								healthy++
							}
						}
						output.F.Printf("  %s Pre-flight passed (%d checks)\n", output.Green("✓"), healthy)
					}
				}

				if dryRun {
					if flagJSON {
						checks := runPreflightChecks(repoArg)
						return output.JSON(map[string]any{
							"dryRun": true,
							"checks": checks,
						})
					}
					output.F.Success("Dry run complete — pre-flight passed")
					return nil
				}

				// Single repo
				req := api.InvestigateRequest{
					RepoName:  repoArg,
					Model:     model,
					ChunkSize: chunkSize,
				}
				var result any
				if err := client.Post(ctx(), "/investigate/single", req, &result); err != nil {
					return err
				}
				if flagJSON {
					return output.JSON(result)
				}
				output.Successf("Investigation started for %s", output.Bold(repoArg))
				return nil
			}

			if all {
				// Fetch enabled repos from API (not the worker's internal repos.json)
				var enabledReposList []struct {
					Name    string `json:"name"`
					Enabled bool   `json:"enabled"`
				}
				if err := client.Get(ctx(), "/repos", &enabledReposList); err != nil {
					return fmt.Errorf("fetching repos: %w", err)
				}

				// Filter to enabled repos
				var enabledRepos []string
				for _, r := range enabledReposList {
					if r.Enabled {
						enabledRepos = append(enabledRepos, r.Name)
					}
				}

				if len(enabledRepos) == 0 {
					return fmt.Errorf("no enabled repos found\n  Add repos first: reposwarm repos add <name> --url <url> --source GitHub")
				}

				// Pre-flight (check once, not per repo)
				if !force {
					checks := runPreflightChecks("")
					failed := 0
					for _, c := range checks {
						if c.Status == "fail" {
							failed++
						}
					}
					if failed > 0 {
						if flagJSON {
							return output.JSON(map[string]any{
								"error":  "pre-flight failed",
								"checks": checks,
							})
						}
						output.F.Error("Cannot start: pre-flight checks failed")
						for _, c := range checks {
							switch c.Status {
							case "ok":
								output.F.Printf("  %s %s: %s\n", output.Green("[OK]"), c.Name, c.Message)
							case "fail":
								output.F.Printf("  %s %s: %s\n", output.Red("[FAIL]"), c.Name, c.Message)
							}
						}
						return fmt.Errorf("pre-flight failed (%d issues)", failed)
					}
					if !flagJSON {
						healthy := 0
						for _, c := range checks {
							if c.Status == "ok" {
								healthy++
							}
						}
						output.F.Printf("  %s Pre-flight passed (%d checks)\n", output.Green("✓"), healthy)
					}
				}

				if dryRun {
					if flagJSON {
						return output.JSON(map[string]any{"dryRun": true, "repos": enabledRepos})
					}
					output.Successf("Dry run: would investigate %d repos: %s", len(enabledRepos), strings.Join(enabledRepos, ", "))
					return nil
				}

				// Start individual investigations for each enabled repo
				started := 0
				for _, repoName := range enabledRepos {
					req := api.InvestigateRequest{
						RepoName:  repoName,
						Model:     model,
						ChunkSize: chunkSize,
					}
					var result any
					if err := client.Post(ctx(), "/investigate/single", req, &result); err != nil {
						if !flagJSON {
							output.F.Warning(fmt.Sprintf("Failed to start %s: %v", repoName, err))
						}
						continue
					}
					started++
					if !flagJSON {
						output.Successf("Investigation started for %s", output.Bold(repoName))
					}
				}

				if flagJSON {
					return output.JSON(map[string]any{
						"started": started,
						"total":   len(enabledRepos),
						"repos":   enabledRepos,
					})
				}
				if started == 0 {
					return fmt.Errorf("failed to start any investigations")
				}
				output.F.Println()
				output.Successf("Started %d/%d investigations", started, len(enabledRepos))
				return nil
			}

			return fmt.Errorf("specify a repo name or use --all\n\nExamples:\n  reposwarm investigate my-repo\n  reposwarm investigate --all")
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Investigate all enabled repos")
	cmd.Flags().StringVar(&model, "model", "", "Model ID (default from config)")
	cmd.Flags().IntVar(&chunkSize, "chunk-size", 0, "Files per chunk (default from config)")
	cmd.Flags().IntVar(&parallel, "parallel", 3, "Parallel limit (daily only)")
	cmd.Flags().BoolVar(&force, "force", false, "Skip pre-flight checks")
	cmd.Flags().BoolVar(&replace, "replace", false, "Terminate existing workflow for this repo before starting")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Run pre-flight only, don't create workflow")
	return cmd
}
