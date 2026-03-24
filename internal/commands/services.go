package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reposwarm/reposwarm-cli/internal/api"
	"github.com/reposwarm/reposwarm-cli/internal/bootstrap"
	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

var knownServices = []string{"api", "worker", "temporal", "ui"}

// resolveInstallDir finds the local installation directory.
// Checks the configured installDir first, then tries cwd and cwd/reposwarm.
// Supports both local (api/ + worker/ subdirs) and Docker-only (temporal/docker-compose.yml) installs.
// Updates config if found in an alternative location.
func resolveInstallDir(cfg *config.Config) (string, error) {
	installDir := cfg.EffectiveInstallDir()
	if flagVerbose {
		output.F.Info(fmt.Sprintf("[verbose] Checking installDir: %s", installDir))
		output.F.Info(fmt.Sprintf("[verbose]   IsLocalInstall (api/ or worker/): %v", bootstrap.IsLocalInstall(installDir)))
		output.F.Info(fmt.Sprintf("[verbose]   IsDockerInstall (temporal/docker-compose.yml): %v", bootstrap.IsDockerInstall(installDir)))
	}
	if bootstrap.IsLocalInstall(installDir) || bootstrap.IsDockerInstall(installDir) {
		return installDir, nil
	}

	cwd, _ := os.Getwd()
	for _, candidate := range []string{cwd, filepath.Join(cwd, "reposwarm")} {
		if flagVerbose {
			output.F.Info(fmt.Sprintf("[verbose] Checking candidate: %s", candidate))
			output.F.Info(fmt.Sprintf("[verbose]   IsLocalInstall: %v, IsDockerInstall: %v", bootstrap.IsLocalInstall(candidate), bootstrap.IsDockerInstall(candidate)))
		}
		if bootstrap.IsLocalInstall(candidate) || bootstrap.IsDockerInstall(candidate) {
			cfg.InstallDir = candidate
			_ = config.Save(cfg)
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no local installation found at %s (checked for api/, worker/ subdirs and temporal/docker-compose.yml)\nRun 'reposwarm new --local' to set up, or set it with: reposwarm config set installDir /path/to/install", installDir)
}

func newServicesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "services",
		Short: "Show all running RepoSwarm services",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check if this is a Docker install — use docker compose ps directly
			cfg, _ := config.Load()
			if cfg != nil {
				installDir := cfg.EffectiveInstallDir()
				if (cfg.IsDockerInstall() || bootstrap.IsDockerInstall(installDir)) {
					return showDockerServices(installDir)
				}
			}

			client, err := getClient()
			if err != nil {
				return err
			}

			var services []api.ServiceInfo
			if err := client.Get(ctx(), "/services", &services); err != nil {
				return fmt.Errorf("failed to list services: %w", err)
			}

			if flagJSON {
				return output.JSON(services)
			}

			F := output.F
			running := 0
			for _, s := range services {
				if s.Status == "running" {
					running++
				}
			}
			F.Section(fmt.Sprintf("Services (%d/%d running)", running, len(services)))

			headers := []string{"Service", "PID", "Status", "Port", "Manager"}
			var rows [][]string
			for _, s := range services {
				pid := "—"
				if s.PID > 0 {
					pid = fmt.Sprint(s.PID)
				}
				statusStr := s.Status
				switch s.Status {
				case "running":
					statusStr = output.Green("running")
				case "stopped":
					statusStr = output.Dim("stopped")
				}
				port := "—"
				if s.Port > 0 {
					port = fmt.Sprint(s.Port)
				}
				manager := s.Manager
				if manager == "" {
					manager = "—"
				}
				rows = append(rows, []string{s.Name, pid, statusStr, port, manager})
			}

			output.Table(headers, rows)
			F.Println()
			return nil
		},
	}
	return cmd
}

func showDockerServices(installDir string) error {
	services, err := bootstrap.DockerComposeServices(installDir)
	if err != nil {
		return fmt.Errorf("failed to list Docker services: %w", err)
	}

	if flagJSON {
		return output.JSON(services)
	}

	F := output.F
	running := 0
	for _, s := range services {
		if s.State == "running" {
			running++
		}
	}
	F.Section(fmt.Sprintf("Services (%d/%d running) — Docker Compose", running, len(services)))

	cfg, _ := config.Load()

	headers := []string{"Service", "Container", "Status", "Health", "URL"}
	var rows [][]string
	for _, s := range services {
		stateStr := s.State
		switch s.State {
		case "running":
			stateStr = output.Green("running")
		case "exited":
			stateStr = output.Red("exited")
		case "restarting":
			stateStr = output.Yellow("restarting")
		}
		health := s.Health
		if health == "" {
			health = "—"
		} else if health == "healthy" {
			health = output.Green("healthy")
		} else if health == "unhealthy" {
			health = output.Red("unhealthy")
		}

		// Build URL from service name and config ports
		url := "—"
		if s.State == "running" && cfg != nil {
			switch s.Service {
			case "ui":
				url = fmt.Sprintf("http://localhost:%s", cfg.EffectiveUIPort())
			case "api":
				url = fmt.Sprintf("http://localhost:%s", cfg.EffectiveAPIPort())
			case "temporal-ui":
				url = fmt.Sprintf("http://localhost:%s", cfg.EffectiveTemporalUIPort())
			}
		}

		rows = append(rows, []string{s.Service, s.Name, stateStr, health, url})
	}

	output.Table(headers, rows)
	F.Println()
	return nil
}
func newRestartCmd() *cobra.Command {
	var wait bool
	var timeout int
	var local bool

	cmd := &cobra.Command{
		Use:   "restart [service]",
		Short: "Restart one or all RepoSwarm services",
		Long: `Restart a RepoSwarm service or all services. Tries the API first,
falls back to local process management if the API is unreachable.

Examples:
  reposwarm restart           # Restart all services
  reposwarm restart worker    # Restart the worker
  reposwarm restart api       # Restart the API server
  reposwarm restart --local   # Force local restart`,
		Args: friendlyMaxArgs(1, "reposwarm restart [service]\n\nServices: api, worker, temporal, ui\n\nExample:\n  reposwarm restart worker"),
		RunE: func(cmd *cobra.Command, args []string) error {
			services := knownServices
			if len(args) > 0 {
				svc := args[0]
				if !isKnownService(svc) {
					return fmt.Errorf("unknown service: %s (must be one of: %s)",
						svc, strings.Join(knownServices, ", "))
				}
				services = []string{svc}
			}

			// For Docker installs, always use local restart (docker compose up --force-recreate)
			// The API restart endpoint uses plain "docker restart" which doesn't re-read
			// env_file changes. Local restart uses --force-recreate which applies new env vars.
			cfg, cfgErr := config.Load()
			isDocker := cfgErr == nil && (cfg.IsDockerInstall() || bootstrap.IsDockerInstall(cfg.EffectiveInstallDir()))
			if isDocker {
				local = true
			}

			// Try API first
			if !local {
				client, err := getClient()
				if err == nil {
					apiWorked := true
					var results []map[string]any
					for _, svc := range services {
						var resp map[string]any
						err := client.Post(ctx(), "/services/"+svc+"/restart", nil, &resp)

						result := map[string]any{"service": svc}
						if err != nil {
							result["status"] = "error"
							result["error"] = err.Error()
							apiWorked = false
						} else {
							result["status"] = resp["status"]
							result["pid"] = resp["pid"]
						}
						results = append(results, result)

						if !flagJSON {
							if err != nil {
								output.F.Printf("  %s %s: %v\n", output.Red("✗"), svc, err)
							} else {
								output.F.Printf("  %s %s restarted", output.Green("✓"), svc)
								if pid, ok := resp["pid"]; ok && pid != nil && pid != float64(0) {
									output.F.Printf(" (PID %.0f)", pid)
								}
								fmt.Println()
							}
						}
					}

					if apiWorked {
						if wait && !flagJSON {
							output.F.Info("Waiting for healthy...")
							deadline := time.Now().Add(time.Duration(timeout) * time.Second)
							for time.Now().Before(deadline) {
								_, err := client.Health(ctx())
								if err == nil {
									output.Successf("Services healthy")
									break
								}
								time.Sleep(2 * time.Second)
							}
						}
						if flagJSON {
							return output.JSON(results)
						}
						return nil
					}
					// Some services failed via API — fall through to local
					if !flagJSON {
						output.F.Warning("Some services failed via API, falling back to local restart...")
					}
				} else if !flagJSON {
					output.F.Warning("API unreachable, using local restart...")
				}
			}

			// Local fallback
			for _, svc := range services {
				if err := restartLocalWithWait(svc, wait, timeout); err != nil {
					if !flagJSON {
						output.F.Printf("  %s %s: %v\n", output.Red("✗"), svc, err)
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&wait, "wait", true, "Wait for service to be healthy after restart")
	cmd.Flags().IntVar(&timeout, "timeout", 30, "Max seconds to wait for healthy")
	cmd.Flags().BoolVar(&local, "local", false, "Force local restart (skip API)")
	return cmd
}

func newStartCmd() *cobra.Command {
	var wait bool
	var local bool

	cmd := &cobra.Command{
		Use:   "start [service]",
		Short: "Start a RepoSwarm service",
		Long: `Start a RepoSwarm service. Tries the API first, falls back to local
process management if the API is unreachable (e.g. when starting the API itself).

Use --local to force local mode (skip API, spawn process directly).

Examples:
  reposwarm start api           # Start the API (uses local fallback automatically)
  reposwarm start worker        # Start the worker
  reposwarm start --local api   # Force local start (skip API call)`,
		Args:  friendlyExactArgs(1, "reposwarm start <service>\n\nServices: api, worker, temporal, ui\n\nExample:\n  reposwarm start worker"),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := args[0]
			if !isKnownService(svc) {
				return fmt.Errorf("unknown service: %s", svc)
			}

			// Try API first (unless --local or starting the API itself)
			if !local && svc != "api" {
				client, err := getClient()
				if err == nil {
					var resp map[string]any
					if err := client.Post(ctx(), "/services/"+svc+"/start", nil, &resp); err == nil {
						if flagJSON {
							return output.JSON(resp)
						}
						output.Successf("%s started (via API)", svc)
						if wait {
							return waitForHealthy(client, svc, 30)
						}
						return nil
					}
					// API call failed — fall through to local
					if !flagJSON {
						output.F.Warning(fmt.Sprintf("API call failed, falling back to local start..."))
					}
				}
			}

			// Local fallback: start the process directly
			return startLocal(svc, wait)
		},
	}

	cmd.Flags().BoolVar(&wait, "wait", true, "Wait for service to be healthy")
	cmd.Flags().BoolVar(&local, "local", false, "Force local start (skip API, spawn process directly)")
	return cmd
}

func newStopCmd() *cobra.Command {
	var local bool

	cmd := &cobra.Command{
		Use:   "stop [service]",
		Short: "Stop one or all RepoSwarm services",
		Long: `Stop a RepoSwarm service or all services. Tries the API first, falls back to local
process management if the API is unreachable.

Examples:
  reposwarm stop              # Stop all services
  reposwarm stop worker       # Stop only the worker
  reposwarm stop --local api  # Force local stop`,
		Args:  friendlyMaxArgs(1, "reposwarm stop [service]\n\nServices: api, worker, temporal, ui\n\nExamples:\n  reposwarm stop         # Stop all services\n  reposwarm stop worker  # Stop specific service"),
		RunE: func(cmd *cobra.Command, args []string) error {
			// If no service specified, stop all services
			var services []string
			if len(args) == 0 {
				services = knownServices
			} else {
				svc := args[0]
				if !isKnownService(svc) {
					return fmt.Errorf("unknown service: %s (must be one of: %s)",
						svc, strings.Join(knownServices, ", "))
				}
				services = []string{svc}
			}

			// Try API first (unless --local)
			var results []map[string]any
			stoppedCount := 0

			for _, svc := range services {
				result := map[string]any{"service": svc}

				// Try API first (unless --local or stopping the API itself)
				if !local && svc != "api" {
					client, err := getClient()
					if err == nil {
						var resp map[string]any
						if err := client.Post(ctx(), "/services/"+svc+"/stop", nil, &resp); err == nil {
							status, _ := resp["status"].(string)
							result["status"] = status

							if flagJSON {
								results = append(results, result)
							} else {
								if status == "stopped" {
									output.Successf("%s stopped", svc)
									stoppedCount++
								} else if status == "not_found" {
									output.F.Info(fmt.Sprintf("%s is not running", svc))
								} else {
									output.F.Error(fmt.Sprintf("%s: unexpected status: %s", svc, status))
								}
							}
							continue
						}
						if !flagJSON && len(services) == 1 {
							output.F.Warning("API call failed, falling back to local stop...")
						}
					}
				}

				// Local fallback
				if err := stopLocal(svc); err != nil {
					result["status"] = "error"
					result["error"] = err.Error()
					if !flagJSON {
						output.F.Printf("  %s %s: %v\n", output.Red("✗"), svc, err)
					}
				} else {
					result["status"] = "stopped"
					stoppedCount++
				}
				if flagJSON {
					results = append(results, result)
				}
			}

			if flagJSON {
				return output.JSON(map[string]any{
					"stopped": stoppedCount,
					"total":   len(services),
					"results": results,
				})
			}

			if len(services) > 1 && stoppedCount > 0 {
				output.F.Println()
				output.Successf("Stopped %d/%d services", stoppedCount, len(services))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&local, "local", false, "Force local stop (skip API, use PID files)")
	return cmd
}

// startLocal starts a service by spawning the process directly.
func startLocal(svc string, wait bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	installDir, err := resolveInstallDir(cfg)
	if err != nil {
		return err
	}

	bsCfg := toBsConfig(cfg)

	if !flagJSON {
		output.F.Info(fmt.Sprintf("Starting %s locally from %s...", svc, installDir))
	}

	if err := bootstrap.LocalStart(installDir, svc, bsCfg); err != nil {
		return err
	}

	if flagJSON {
		return output.JSON(map[string]any{"service": svc, "status": "started", "mode": "local"})
	}

	output.Successf("%s started (local)", svc)

	if wait && svc == "api" {
		output.F.Info("Waiting for API to be healthy...")
		if err := waitForLocalHealth(fmt.Sprintf("http://localhost:%s/v1/health", bsCfg.APIPort), 30); err != nil {
			output.F.Warning("API not responding after 30s — check api/api.log")
		} else {
			output.Successf("API is healthy")
		}
	}
	return nil
}

// stopLocal stops a service using PID files.
func stopLocal(svc string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	installDir, err := resolveInstallDir(cfg)
	if err != nil {
		return err
	}

	bsCfg := toBsConfig(cfg)
	if err := bootstrap.LocalStop(installDir, svc, bsCfg); err != nil {
		return err
	}

	if flagJSON {
		return output.JSON(map[string]any{"service": svc, "status": "stopped", "mode": "local"})
	}
	output.Successf("%s stopped (local)", svc)
	return nil
}

// restartLocal restarts a service locally.
func restartLocal(svc string) error {
	return restartLocalWithWait(svc, false, 0)
}

func restartLocalWithWait(svc string, wait bool, timeout int) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	installDir, err := resolveInstallDir(cfg)
	if err != nil {
		return err
	}

	bsCfg := toBsConfig(cfg)
	if err := bootstrap.LocalRestart(installDir, svc, bsCfg); err != nil {
		return err
	}

	if !flagJSON {
		output.Successf("%s restarted (local)", svc)
	}

	// Wait for Docker container health if requested
	if wait && (cfg.IsDockerInstall() || bootstrap.IsDockerInstall(installDir)) {
		if !flagJSON {
			output.F.Info(fmt.Sprintf("Waiting for %s to be healthy...", svc))
		}
		if err := bootstrap.WaitForDockerHealth(installDir, svc, timeout); err != nil {
			return err
		}
		if !flagJSON {
			output.Successf("%s is healthy", svc)
		}

		// Additional verification for worker: check env vars loaded
		if svc == "worker" {
			// Wait a bit for worker to fully initialize
			time.Sleep(3 * time.Second)

			// Check for env errors via API
			client, err := getClient()
			if err == nil {
				workers := gatherWorkerInfo(client)
				for _, w := range workers {
					// Filter out false-positive errors (e.g. ANTHROPIC_API_KEY on Bedrock)
					filteredErrors := filterEnvErrors(w.EnvErrors)
					if len(filteredErrors) > 0 {
						if !flagJSON {
							output.F.Warning(fmt.Sprintf("Worker restarted but has env errors: %s", strings.Join(filteredErrors, ", ")))
							output.F.Warning("Run: reposwarm doctor to diagnose")
						}
						break
					}
				}
			}
		}
	}

	return nil
}

func toBsConfig(cfg *config.Config) *bootstrap.Config {
	bsCfg := &bootstrap.Config{
		APIPort:        cfg.EffectiveAPIPort(),
		UIPort:         cfg.EffectiveUIPort(),
		TemporalPort:   cfg.EffectiveTemporalPort(),
		TemporalUIPort: cfg.EffectiveTemporalUIPort(),
		DynamoDBTable:  cfg.EffectiveDynamoDBTable(),
		DefaultModel:   cfg.EffectiveModel(),
		Region:         cfg.Region,
		WorkerRepoURL:  cfg.EffectiveWorkerRepoURL(),
		APIRepoURL:     cfg.EffectiveAPIRepoURL(),
		UIRepoURL:      cfg.EffectiveUIRepoURL(),
	}
	// Include provider env vars so worker gets CLAUDE_CODE_USE_BEDROCK etc.
	bsCfg.ProviderEnvVars = config.WorkerEnvVars(&cfg.ProviderConfig, cfg.EffectiveModel())
	return bsCfg
}

func waitForLocalHealth(url string, timeoutSec int) error {
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		_, err := (&api.Client{}).Health(ctx())
		if err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout")
}

func waitForHealthy(client *api.Client, svc string, timeoutSec int) error {
	output.F.Info("Waiting for healthy...")
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		health, err := client.Health(ctx())
		if err == nil && (svc != "worker" || health.Worker.Connected) {
			output.Successf("%s healthy", svc)
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	output.F.Warning("Health check timed out")
	return nil
}

func isKnownService(name string) bool {
	for _, s := range knownServices {
		if s == name {
			return true
		}
	}
	if strings.HasPrefix(name, "worker-") {
		return true
	}
	return false
}
