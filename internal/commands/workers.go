package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/reposwarm/reposwarm-cli/internal/api"
	"github.com/reposwarm/reposwarm-cli/internal/bootstrap"
	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newWorkersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workers",
		Aliases: []string{"worker"},
		Short:   "Manage and inspect workers",
	}
	cmd.AddCommand(newWorkersListCmd())
	cmd.AddCommand(newWorkersShowCmd())
	return cmd
}

func newWorkersListCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all workers with health and activity status",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			var resp api.WorkersResponse
			if err := client.Get(ctx(), "/workers", &resp); err != nil {
				return fmt.Errorf("failed to list workers: %w", err)
			}

			// For Docker installs, overlay real container status
			cfg, _ := config.Load()
			if cfg != nil {
				installDir := cfg.EffectiveInstallDir()
				if (cfg.IsDockerInstall() || bootstrap.IsDockerInstall(installDir)) {
					dockerServices, _ := bootstrap.DockerComposeServices(installDir)
					for i, w := range resp.Workers {
						for _, ds := range dockerServices {
							if ds.Service == "worker" {
								if ds.State == "running" {
									resp.Workers[i].Status = "healthy"
									if ds.Health == "healthy" {
										resp.Workers[i].Status = "healthy"
									} else if ds.Health == "unhealthy" {
										resp.Workers[i].Status = "degraded"
									}
								}
								resp.Workers[i].Host = ds.Name
								_ = w // suppress unused
								break
							}
						}
					}
					// Also fix env errors using worker.env file
					workerEnv, _ := bootstrap.ReadWorkerEnvFile(installDir)
					if workerEnv != nil {
						for i := range resp.Workers {
							var remaining []string
							for _, e := range resp.Workers[i].EnvErrors {
								// Check if the "missing" env var is actually in worker.env
								found := false
								for k := range workerEnv {
									if strings.Contains(e, k) {
										found = true
										break
									}
								}
								if !found {
									remaining = append(remaining, e)
								}
							}
							resp.Workers[i].EnvErrors = remaining
						}
					}
					// Recount healthy
					resp.Healthy = 0
					for _, w := range resp.Workers {
						if w.Status == "healthy" {
							resp.Healthy++
						}
					}
				}
			}

			if flagJSON {
				return output.JSON(resp)
			}

			F := output.F
			F.Section(fmt.Sprintf("Workers (%d configured, %d healthy)", resp.Total, resp.Healthy))

			if len(resp.Workers) == 0 {
				F.Warning("No workers detected")
				F.Info("Workers register with Temporal when they start.")
				F.Info("Check: reposwarm logs worker")
				return nil
			}

			headers := []string{"Name", "Status", "Queue", "Current Task", "Last Activity", "Env"}
			if verbose {
				headers = append(headers, "PID", "Host")
			}
			var rows [][]string
			for _, w := range resp.Workers {
				statusStr := formatWorkerStatus(w.Status)
				envStr := output.Green("OK")

				// Filter out false positive env errors based on provider config
				filteredErrors := filterEnvErrors(w.EnvErrors)
				if len(filteredErrors) > 0 {
					envStr = output.Red(fmt.Sprintf("%d env errors", len(filteredErrors)))
				}
				currentTask := w.CurrentTask
				if currentTask == "" {
					currentTask = output.Dim("idle")
				}
				lastAct := w.LastActivity
				if lastAct == "" {
					// Try to get last activity from completed workflows
					lastAct = getLastWorkflowTime(client)
				}
				if lastAct == "" {
					lastAct = output.Dim("never")
				}

				row := []string{w.Name, statusStr, w.TaskQueue, currentTask, lastAct, envStr}
				if verbose {
					pid := "—"
					if w.PID > 0 {
						pid = fmt.Sprint(w.PID)
					}
					host := w.Host
					if host == "" {
						host = "—"
					}
					row = append(row, pid, host)
				}
				rows = append(rows, row)
			}

			output.Table(headers, rows)
			F.Println()
			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Include PID, uptime, host")
	return cmd
}

func newWorkersShowCmd() *cobra.Command {
	var logLines int
	var noLogs bool

	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Deep-dive on a single worker: env, logs, current task",
		Args:  friendlyExactArgs(1, "reposwarm workers show <name>\n\nExample:\n  reposwarm workers show worker-1"),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetName := args[0]
			client, err := getClient()
			if err != nil {
				return err
			}

			// Get worker detail
			var worker api.WorkerInfo
			if err := client.Get(ctx(), "/workers/"+targetName, &worker); err != nil {
				return fmt.Errorf("worker '%s' not found: %w", targetName, err)
			}

			if flagJSON {
				result := map[string]any{"worker": worker}
				if !noLogs {
					var logResp struct {
						Lines []string `json:"lines"`
					}
					if err := client.Get(ctx(), fmt.Sprintf("/services/worker/logs?lines=%d", logLines), &logResp); err == nil {
						result["logs"] = logResp.Lines
					}
				}
				return output.JSON(result)
			}

			// For Docker installs, overlay real container status
			cfg, _ := config.Load()
			if cfg != nil && (cfg.IsDockerInstall() || bootstrap.IsDockerInstall(cfg.EffectiveInstallDir())) {
				dockerServices, _ := bootstrap.DockerComposeServices(cfg.EffectiveInstallDir())
				for _, ds := range dockerServices {
					if ds.Service == "worker" && ds.State == "running" {
						worker.Status = "healthy"
						if ds.Health == "unhealthy" {
							worker.Status = "degraded"
						}
						worker.Host = ds.Name
						break
					}
				}
			}

			F := output.F
			F.Section(fmt.Sprintf("Worker: %s", worker.Name))
			F.KeyValue("Status", formatWorkerStatus(worker.Status))
			F.KeyValue("Identity", worker.Identity)
			if worker.PID > 0 {
				F.KeyValue("PID", fmt.Sprint(worker.PID))
			}
			F.KeyValue("Task Queue", worker.TaskQueue)
			F.KeyValue("Host", orDash(worker.Host))
			F.KeyValue("Current Task", orDash(worker.CurrentTask))
			F.KeyValue("Last Activity", orDash(worker.LastActivity))
			if worker.Model != "" {
				F.KeyValue("Model", worker.Model)
			}

			// Environment — prefer Docker container env or worker.env for Docker installs
			F.Println()
			F.Section("Environment")
			isDocker := cfg != nil && (cfg.IsDockerInstall() || bootstrap.IsDockerInstall(cfg.EffectiveInstallDir()))

			if isDocker {
				// Read actual env from worker.env + container
				workerEnv, _ := bootstrap.ReadWorkerEnvFile(cfg.EffectiveInstallDir())
				containerEnv, _ := bootstrap.DockerServiceEnv(cfg.EffectiveInstallDir(), "worker")
				// Merge: container env overrides worker.env
				merged := make(map[string]string)
				for k, v := range workerEnv {
					merged[k] = v
				}
				for k, v := range containerEnv {
					merged[k] = v
				}

				importantVars := []string{
					"CLAUDE_CODE_USE_BEDROCK", "ANTHROPIC_API_KEY", "ANTHROPIC_MODEL",
					"AWS_REGION", "AWS_ACCESS_KEY_ID", "AWS_BEARER_TOKEN_BEDROCK",
					"GITHUB_TOKEN", "TEMPORAL_SERVER_URL", "DYNAMODB_ENDPOINT",
				}
				for _, key := range importantVars {
					val, ok := merged[key]
					if ok && val != "" {
						source := "worker.env"
						if _, inContainer := containerEnv[key]; inContainer {
							source = "container"
						}
						F.Printf("  %s %s: set (%s)\n", output.Green("[OK]"), key, source)
					} else {
						// Only flag as FAIL for critical vars
						if key == "ANTHROPIC_API_KEY" || key == "GITHUB_TOKEN" || key == "AWS_ACCESS_KEY_ID" || key == "AWS_BEARER_TOKEN_BEDROCK" {
							// These are optional depending on auth method
							F.Printf("  %s %s: not set\n", output.Dim("[—]"), key)
						} else if key == "CLAUDE_CODE_USE_BEDROCK" || key == "ANTHROPIC_MODEL" {
							F.Printf("  %s %s: NOT SET\n", output.Red("[FAIL]"), key)
						} else {
							F.Printf("  %s %s: set (default)\n", output.Green("[OK]"), key)
						}
					}
				}
			} else {
			var envResp struct {
				Entries []struct {
					Key    string `json:"key"`
					Value  string `json:"value"`
					Source string `json:"source"`
					Set    bool   `json:"set"`
				} `json:"entries"`
			}
			if err := client.Get(ctx(), "/workers/"+targetName+"/env", &envResp); err == nil {
				for _, e := range envResp.Entries {
					if !e.Set && !strings.Contains(e.Key, "ANTHROPIC") && !strings.Contains(e.Key, "GITHUB") && !strings.Contains(e.Key, "AWS_ACCESS") {
						continue // Only show important unset vars
					}
					if e.Set {
						F.Printf("  %s %s: set (%s)\n", output.Green("[OK]"), e.Key, e.Source)
					} else {
						F.Printf("  %s %s: NOT SET\n", output.Red("[FAIL]"), e.Key)
					}
				}
			}
			} // end else (non-Docker)

			// Logs from API
			if !noLogs {
				F.Println()
				var logResp struct {
					Lines []string `json:"lines"`
				}
				if err := client.Get(ctx(), fmt.Sprintf("/services/worker/logs?lines=%d", logLines), &logResp); err == nil && len(logResp.Lines) > 0 {
					F.Section(fmt.Sprintf("Recent Logs (last %d lines)", logLines))
					for _, l := range logResp.Lines {
						F.Printf("  %s\n", l)
					}
				} else {
					F.Section("Logs")
					F.Info("No worker log file found")
				}
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&logLines, "logs", 10, "Number of log tail lines to include")
	cmd.Flags().BoolVar(&noLogs, "no-logs", false, "Skip log section")
	return cmd
}

// gatherWorkerInfo fetches worker info from the API.
func gatherWorkerInfo(client *api.Client) []api.WorkerInfo {
	var resp api.WorkersResponse
	if err := client.Get(ctx(), "/workers", &resp); err != nil {
		return nil
	}
	return resp.Workers
}

func formatWorkerStatus(status string) string {
	switch status {
	case "healthy":
		return output.Green("✅ healthy")
	case "degraded":
		return output.Yellow("⚠ degraded")
	case "failed":
		return output.Red("❌ failed")
	case "stopped":
		return output.Dim("⏹ stopped")
	default:
		return status
	}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// filterEnvErrors removes false-positive env errors based on current provider config.
// The API reports missing vars without provider context; the CLI knows the real requirements.
func filterEnvErrors(errors []string) []string {
	cfg, err := config.Load()
	if err != nil {
		return errors // can't determine provider, return as-is
	}

	provider := cfg.EffectiveProvider()
	var filtered []string

	for _, e := range errors {
		skip := false
		switch provider {
		case config.ProviderBedrock:
			// Bedrock doesn't need ANTHROPIC_API_KEY; model is set via worker.env
			if e == "ANTHROPIC_API_KEY" || e == "ANTHROPIC_MODEL" {
				skip = true
			}
		case config.ProviderLiteLLM:
			// LiteLLM doesn't need ANTHROPIC_API_KEY
			if e == "ANTHROPIC_API_KEY" {
				skip = true
			}
		}
		if !skip {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// getLastWorkflowTime returns the most recent workflow close time, or "" if none found.
func getLastWorkflowTime(client *api.Client) string {
	if client == nil {
		return ""
	}
	var wfResp struct {
		Executions []struct {
			CloseTime string `json:"closeTime"`
			StartTime string `json:"startTime"`
		} `json:"executions"`
	}
	if err := client.Get(ctx(), "/workflows?status=Completed&limit=1", &wfResp); err != nil {
		return ""
	}
	if len(wfResp.Executions) > 0 {
		t := wfResp.Executions[0].CloseTime
		if t == "" {
			t = wfResp.Executions[0].StartTime
		}
		if t != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
				return parsed.Format("2006-01-02 15:04")
			}
			// Try RFC3339 without nano
			if parsed, err := time.Parse(time.RFC3339, t); err == nil {
				return parsed.Format("2006-01-02 15:04")
			}
			return t
		}
	}
	return ""
}
