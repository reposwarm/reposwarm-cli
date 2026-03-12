package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/reposwarm/reposwarm-cli/internal/bootstrap"
	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var tail bool
	var lines int
	var since string

	cmd := &cobra.Command{
		Use:   "logs [service]",
		Short: "View service logs via API",
		Long: `View logs for a RepoSwarm service.

Available services: api, worker, temporal, ui

If no service is specified, shows logs from all services.`,
		Args: friendlyMaxArgs(1, "reposwarm logs [service]\n\nServices: api, worker, temporal, ui\n\nExample:\n  reposwarm logs worker -n 100"),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check for Docker install — use docker compose logs directly
			cfg, _ := config.Load()
			if cfg != nil && (cfg.IsDockerInstall() || bootstrap.IsDockerInstall(cfg.EffectiveInstallDir())) {
				return showDockerLogs(cfg.EffectiveInstallDir(), args, lines, tail, since)
			}

			client, err := getClient()
			if err != nil {
				return err
			}

			services := []string{"api", "worker", "temporal", "ui"}
			if len(args) > 0 {
				svc := args[0]
				valid := false
				for _, s := range services {
					if s == svc {
						valid = true
						break
					}
				}
				if !valid {
					return fmt.Errorf("invalid service: %s (must be one of: api, worker, temporal, ui)", svc)
				}
				services = []string{svc}
			}

			if tail {
				// Follow mode: poll every 2s
				for {
					for _, svc := range services {
						var resp struct {
							Service string   `json:"service"`
							Lines   []string `json:"lines"`
							Total   int      `json:"total"`
						}
						path := fmt.Sprintf("/services/%s/logs?lines=%d", svc, lines)
						if err := client.Get(ctx(), path, &resp); err != nil {
							continue
						}
						for _, l := range resp.Lines {
							if len(services) > 1 {
								fmt.Printf("[%s] %s\n", output.Cyan(svc), l)
							} else {
								fmt.Println(l)
							}
						}
					}
					time.Sleep(2 * time.Second)
				}
			}

			// Non-follow: single fetch
			for _, svc := range services {
				var resp struct {
					Service string   `json:"service"`
					LogFile *string  `json:"logFile"`
					Lines   []string `json:"lines"`
					Total   int      `json:"total"`
				}
				path := fmt.Sprintf("/services/%s/logs?lines=%d", svc, lines)
				if err := client.Get(ctx(), path, &resp); err != nil {
					if !flagJSON {
						output.F.Warning(fmt.Sprintf("Error reading %s logs: %v", svc, err))
					}
					continue
				}

				if flagJSON {
					output.JSON(resp)
					continue
				}

				if len(resp.Lines) == 0 {
					continue
				}

				output.F.Section(fmt.Sprintf("%s logs (%d lines)", svc, len(resp.Lines)))
				for _, l := range resp.Lines {
					fmt.Printf("  %s\n", l)
				}
				output.F.Println()
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&tail, "follow", "f", false, "Follow/stream logs")
	cmd.Flags().IntVarP(&lines, "tail", "n", 200, "Number of lines to show from end of logs")
	cmd.Flags().StringVar(&since, "since", "", "Show logs since timestamp (e.g., 30m, 1h) for Docker installs")
	return cmd
}

func showDockerLogs(installDir string, args []string, lines int, tail bool, since string) error {
	composeDir := filepath.Join(installDir, config.ComposeSubDir)
	if _, err := os.Stat(filepath.Join(composeDir, "docker-compose.yml")); err != nil {
		return fmt.Errorf("docker-compose.yml not found at %s", composeDir)
	}

	cmdArgs := []string{"compose", "logs"}

	// If --since is provided, use it; otherwise use --tail
	if since != "" {
		cmdArgs = append(cmdArgs, "--since", since)
	} else {
		cmdArgs = append(cmdArgs, "--tail", strconv.Itoa(lines))
	}

	if tail {
		cmdArgs = append(cmdArgs, "-f")
	}
	if len(args) > 0 {
		cmdArgs = append(cmdArgs, args[0])
	}

	cmd := exec.Command("docker", cmdArgs...)
	cmd.Dir = composeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
