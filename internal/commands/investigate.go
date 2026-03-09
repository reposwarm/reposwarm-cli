package commands

import (
	"fmt"
	"strings"
	"time"

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
  reposwarm investigate --all --parallel=1  # Sequential (one at a time)
  reposwarm investigate --all --parallel=2  # Two repos at a time
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

				// Check for recent investigations (unless --force)
				var recentlyInvestigated map[string]string // repoName -> time ago string
				if !force {
					recentlyInvestigated = checkRecentInvestigations(client, enabledRepos)
				}

				// When --parallel is explicitly set, configure worker and use sequential/batched dispatch
				parallelSet := cmd.Flags().Changed("parallel")

				if parallelSet {
					// Dynamically configure worker concurrency
					if err := ensureWorkerParallel(client, parallel); err != nil {
						return err
					}
				}

				// Start investigations for each enabled repo
				started := 0
				skipped := 0
				failed := 0
				completed := 0

				if parallelSet && parallel <= 1 {
					// --- Sequential mode: one repo at a time ---
					total := len(enabledRepos)
					for i, rn := range enabledRepos {
						if timeAgo, wasRecent := recentlyInvestigated[rn]; wasRecent {
							skipped++
							if !flagJSON {
								output.F.Printf("  %s Skipping %s (investigated %s)\n",
									output.Dim("⊘"), output.Bold(rn), timeAgo)
							}
							continue
						}

						if !flagJSON {
							output.F.Printf("\n  [%d/%d] Starting %s...\n", i+1, total, output.Bold(rn))
						}

						req := api.InvestigateRequest{
							RepoName:  rn,
							Model:     model,
							ChunkSize: chunkSize,
						}
						var resp api.InvestigateResponse
						if err := client.Post(ctx(), "/investigate/single", req, &resp); err != nil {
							failed++
							if !flagJSON {
								output.F.Warning(fmt.Sprintf("Failed to start %s: %v", rn, err))
							}
							continue
						}
						started++

						// Wait for completion before starting the next repo
						if resp.WorkflowID != "" {
							if !flagJSON {
								output.F.Printf("  %s Waiting for %s to finish...\n", output.Dim("⏳"), output.Bold(rn))
							}
							startTime := time.Now()
							finalStatus, err := waitForWorkflow(client, resp.WorkflowID, 5)
							elapsed := time.Since(startTime).Round(time.Second)
							if err != nil {
								if !flagJSON {
									output.F.Warning(fmt.Sprintf("Error watching %s: %v", rn, err))
								}
							} else if strings.EqualFold(finalStatus, "Completed") {
								completed++
								if !flagJSON {
									output.Successf("%s completed (%s)", output.Bold(rn), elapsed)
								}
							} else {
								if !flagJSON {
									output.F.Warning(fmt.Sprintf("%s finished with status: %s (%s)", rn, finalStatus, elapsed))
								}
							}
						}
					}
				} else if parallelSet && parallel > 1 {
					// --- Batched mode: N repos at a time ---
					total := len(enabledRepos)
					batchIdx := 0
					for batchIdx < total {
						end := batchIdx + parallel
						if end > total {
							end = total
						}
						batch := enabledRepos[batchIdx:end]

						// Start all repos in this batch
						type inflight struct {
							name       string
							workflowID string
							startTime  time.Time
						}
						var active []inflight

						for _, rn := range batch {
							if timeAgo, wasRecent := recentlyInvestigated[rn]; wasRecent {
								skipped++
								if !flagJSON {
									output.F.Printf("  %s Skipping %s (investigated %s)\n",
										output.Dim("⊘"), output.Bold(rn), timeAgo)
								}
								continue
							}

							if !flagJSON {
								output.F.Printf("  [batch %d-%d/%d] Starting %s...\n", batchIdx+1, end, total, output.Bold(rn))
							}

							req := api.InvestigateRequest{
								RepoName:  rn,
								Model:     model,
								ChunkSize: chunkSize,
							}
							var resp api.InvestigateResponse
							if err := client.Post(ctx(), "/investigate/single", req, &resp); err != nil {
								failed++
								if !flagJSON {
									output.F.Warning(fmt.Sprintf("Failed to start %s: %v", rn, err))
								}
								continue
							}
							started++
							if resp.WorkflowID != "" {
								active = append(active, inflight{name: rn, workflowID: resp.WorkflowID, startTime: time.Now()})
							}
						}

						// Wait for all in this batch to complete
						for _, a := range active {
							if !flagJSON {
								output.F.Printf("  %s Waiting for %s...\n", output.Dim("⏳"), output.Bold(a.name))
							}
							finalStatus, err := waitForWorkflow(client, a.workflowID, 5)
							elapsed := time.Since(a.startTime).Round(time.Second)
							if err != nil {
								if !flagJSON {
									output.F.Warning(fmt.Sprintf("Error watching %s: %v", a.name, err))
								}
							} else if strings.EqualFold(finalStatus, "Completed") {
								completed++
								if !flagJSON {
									output.Successf("%s completed (%s)", output.Bold(a.name), elapsed)
								}
							} else {
								if !flagJSON {
									output.F.Warning(fmt.Sprintf("%s finished with status: %s (%s)", a.name, finalStatus, elapsed))
								}
							}
						}

						batchIdx = end
					}
				} else {
					// --- Default mode: fire-and-forget (existing behavior) ---
					for _, repoName := range enabledRepos {
						if timeAgo, wasRecent := recentlyInvestigated[repoName]; wasRecent {
							skipped++
							if !flagJSON {
								output.F.Printf("  %s Skipping %s (investigated %s)\n",
									output.Dim("⊘"), output.Bold(repoName), timeAgo)
							}
							continue
						}

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
				}

				if flagJSON {
					mode := "parallel"
					if parallelSet && parallel <= 1 {
						mode = "sequential"
					} else if parallelSet {
						mode = fmt.Sprintf("batched(%d)", parallel)
					}
					return output.JSON(map[string]any{
						"started":   started,
						"completed": completed,
						"skipped":   skipped,
						"failed":    failed,
						"total":     len(enabledRepos),
						"repos":     enabledRepos,
						"mode":      mode,
					})
				}
				if started == 0 && skipped == 0 {
					return fmt.Errorf("failed to start any investigations")
				}
				output.F.Println()
				if parallelSet {
					output.Successf("Completed %d/%d investigations (%d skipped, %d failed)", completed, len(enabledRepos), skipped, failed)
				} else if skipped > 0 {
					output.Successf("Started %d/%d investigations (%d skipped, use --force to override)", started, len(enabledRepos), skipped)
				} else {
					output.Successf("Started %d/%d investigations", started, len(enabledRepos))
				}
				return nil
			}

			return fmt.Errorf("specify a repo name or use --all\n\nExamples:\n  reposwarm investigate my-repo\n  reposwarm investigate --all")
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Investigate all enabled repos")
	cmd.Flags().StringVar(&model, "model", "", "Model ID (default from config)")
	cmd.Flags().IntVar(&chunkSize, "chunk-size", 0, "Files per chunk (default from config)")
	cmd.Flags().IntVar(&parallel, "parallel", 0, "Max parallel investigations (0=sequential, default=all-at-once when not set)")
	cmd.Flags().BoolVar(&force, "force", false, "Skip pre-flight checks and re-investigate recently completed repos")
	cmd.Flags().BoolVar(&replace, "replace", false, "Terminate existing workflow for this repo before starting")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Run pre-flight only, don't create workflow")
	return cmd
}
