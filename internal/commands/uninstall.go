package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newUninstallCmd() *cobra.Command {
	var forceFlag bool
	var keepConfigFlag bool

	cmd := &cobra.Command{
		Use:     "uninstall",
		Aliases: []string{"remove"},
		Short:   "Remove RepoSwarm from this machine",
		Long: `Remove RepoSwarm components from this machine.

For local installations: stops services, removes Docker containers, deletes data files.
For remote CLI-only setups: removes the CLI binary and config.

Examples:
  reposwarm uninstall                # Interactive removal
  reposwarm uninstall --force        # Skip confirmation
  reposwarm uninstall --keep-config  # Keep ~/.reposwarm config`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := config.Load()
			installDir := ""
			if cfg != nil {
				installDir = cfg.EffectiveInstallDir()
			}

			// Detect if this is a local or remote setup
			isLocal := installDir != "" && dirExists(installDir)

			if flagJSON {
				return output.JSON(map[string]any{
					"installDir": installDir,
					"isLocal":    isLocal,
					"wouldRemove": buildRemovalList(installDir, isLocal, keepConfigFlag),
				})
			}

			output.F.Section("RepoSwarm Uninstall")
			fmt.Println()

			if isLocal {
				fmt.Printf("  Installation type: %s\n", output.Bold("Local (full stack)"))
				fmt.Printf("  Install directory: %s\n", installDir)
			} else {
				fmt.Printf("  Installation type: %s\n", output.Bold("CLI only (remote)"))
				output.F.Warning("This will only remove the CLI binary and config.")
				output.F.Info("The API server, workers, and data on the remote host are NOT affected.")
				output.F.Info("To fully remove a remote installation, SSH into the server and run 'reposwarm uninstall' there.")
			}

			fmt.Println()
			output.F.Section("Will Remove")
			fmt.Println()

			removals := buildRemovalList(installDir, isLocal, keepConfigFlag)
			for _, r := range removals {
				fmt.Printf("  • %s\n", r)
			}
			fmt.Println()

			if !forceFlag && !flagAgent {
				reader := bufio.NewReader(os.Stdin)
				fmt.Printf("  %s Type 'yes' to confirm: ", output.Red("⚠ This cannot be undone."))
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(answer)
				if answer != "yes" {
					fmt.Println()
					output.F.Info("Uninstall cancelled.")
					return nil
				}
			}

			fmt.Println()

			// Execute removal steps
			var errors []string

			if isLocal {
				// 1. Stop all services
				fmt.Print("  Stopping services... ")
				stopErrors := stopAllServices(installDir)
				if len(stopErrors) > 0 {
					fmt.Println(output.Yellow("partial"))
					for _, e := range stopErrors {
						errors = append(errors, e)
					}
				} else {
					fmt.Println(output.Green("done"))
				}

				// 2. Docker compose down (Temporal)
				temporalDir := filepath.Join(installDir, config.ComposeSubDir)
				composePath := filepath.Join(temporalDir, "docker-compose.yml")
				if fileExists(composePath) {
					fmt.Print("  Stopping Temporal containers... ")
					if err := execCmd("docker", "compose", "-f", composePath, "down", "-v"); err != nil {
						fmt.Println(output.Yellow("failed"))
						errors = append(errors, fmt.Sprintf("docker compose down: %v", err))
					} else {
						fmt.Println(output.Green("done"))
					}
				}

				// 3. Remove install directory
				fmt.Printf("  Removing %s... ", installDir)
				if err := os.RemoveAll(installDir); err != nil {
					fmt.Println(output.Yellow("failed"))
					errors = append(errors, fmt.Sprintf("remove install dir: %v", err))
				} else {
					fmt.Println(output.Green("done"))
				}
			}

			// 4. Remove config directory
			if !keepConfigFlag {
				home, _ := os.UserHomeDir()
				configDir := filepath.Join(home, ".reposwarm")
				if dirExists(configDir) {
					fmt.Printf("  Removing %s... ", configDir)
					if err := os.RemoveAll(configDir); err != nil {
						fmt.Println(output.Yellow("failed"))
						errors = append(errors, fmt.Sprintf("remove config: %v", err))
					} else {
						fmt.Println(output.Green("done"))
					}
				}
			}

			// 5. Remove CLI binary
			binPath, err := os.Executable()
			if err == nil {
				binPath, _ = filepath.EvalSymlinks(binPath)
				fmt.Printf("  Removing CLI binary (%s)... ", binPath)
				// We can't delete ourselves while running on some OSes,
				// so we schedule removal
				if err := scheduleRemoval(binPath); err != nil {
					fmt.Println(output.Yellow("manual"))
					fmt.Printf("\n  Remove manually: %s\n", output.Cyan(fmt.Sprintf("sudo rm %s", binPath)))
				} else {
					fmt.Println(output.Green("done"))
				}
			}

			fmt.Println()
			if len(errors) > 0 {
				output.F.Warning(fmt.Sprintf("Completed with %d warning(s):", len(errors)))
				for _, e := range errors {
					fmt.Printf("  • %s\n", e)
				}
				fmt.Println()
			} else {
				output.Successf("RepoSwarm has been removed.")
			}

			if keepConfigFlag {
				output.F.Info("Config preserved at ~/.reposwarm/ (use for reinstall)")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&forceFlag, "force", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&keepConfigFlag, "keep-config", false, "Keep ~/.reposwarm config directory")
	return cmd
}

func buildRemovalList(installDir string, isLocal bool, keepConfig bool) []string {
	var items []string

	if isLocal {
		items = append(items, fmt.Sprintf("Services: stop API, worker, UI processes"))
		temporalDir := filepath.Join(installDir, config.ComposeSubDir)
		if dirExists(temporalDir) {
			items = append(items, "Temporal: docker compose down + remove volumes")
		}
		items = append(items, fmt.Sprintf("Install directory: %s (api, worker, ui, logs)", installDir))
	}

	if !keepConfig {
		home, _ := os.UserHomeDir()
		configDir := filepath.Join(home, ".reposwarm")
		if dirExists(configDir) {
			items = append(items, fmt.Sprintf("Config: %s (config.json, providers.json)", configDir))
		}
	}

	binPath, err := os.Executable()
	if err == nil {
		binPath, _ = filepath.EvalSymlinks(binPath)
		items = append(items, fmt.Sprintf("CLI binary: %s", binPath))
	}

	return items
}

func stopAllServices(installDir string) []string {
	var errors []string
	services := []struct {
		name     string
		patterns []string
	}{
		{"worker", []string{"python.*src.worker", "python.*worker"}},
		{"ui", []string{"next-server", "node.*reposwarm-ui"}},
		{"api", []string{"node.*reposwarm-api", "node.*dist/index"}},
	}

	for _, svc := range services {
		for _, pattern := range svc.patterns {
			out, err := exec.Command("pgrep", "-f", pattern).Output()
			if err != nil {
				continue
			}
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if line == "" {
					continue
				}
				if killErr := exec.Command("kill", line).Run(); killErr != nil {
					errors = append(errors, fmt.Sprintf("kill %s (pid %s): %v", svc.name, line, killErr))
				}
			}
		}
	}

	return errors
}

func execCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

func scheduleRemoval(binPath string) error {
	// On Linux/macOS: unlink works even while binary is running
	return os.Remove(binPath)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
