package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/reposwarm/reposwarm-cli/internal/bootstrap"
	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newTeardownCmd() *cobra.Command {
	var forceFlag bool
	var removeVolumes bool

	cmd := &cobra.Command{
		Use:   "teardown",
		Short: "Stop and remove all local Docker containers and optionally volumes",
		Long: `Stop and remove all RepoSwarm Docker containers for a local installation.

This is a destructive operation — it kills all running containers and removes them.
Use --volumes to also remove Docker volumes (database data, Temporal state).

Your config, CLI binary, and install directory are NOT touched.
Use 'reposwarm new --local' to start fresh after teardown.

Examples:
  reposwarm teardown              # Stop + remove containers (keeps data)
  reposwarm teardown --volumes    # Stop + remove containers AND volumes
  reposwarm teardown --force      # Skip confirmation`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("no config found — is RepoSwarm installed? Run 'reposwarm new --local' first")
			}

			installDir := cfg.EffectiveInstallDir()
			if installDir == "" {
				return fmt.Errorf("no installDir in config — this doesn't appear to be a local installation")
			}

			isDocker := cfg.IsDockerInstall() || bootstrap.IsDockerInstall(installDir)
			if !isDocker {
				return fmt.Errorf("no Docker Compose installation found at %s", installDir)
			}

			composeDir := filepath.Join(installDir, config.ComposeSubDir)
			composePath := filepath.Join(composeDir, "docker-compose.yml")
			if _, err := os.Stat(composePath); err != nil {
				return fmt.Errorf("docker-compose.yml not found at %s\nRun 'reposwarm new --local' to set up", composePath)
			}

			if flagJSON {
				return output.JSON(map[string]any{
					"installDir":    installDir,
					"composeDir":    composeDir,
					"removeVolumes": removeVolumes,
				})
			}

			// Show what we'll do
			output.F.Section("RepoSwarm Teardown")
			fmt.Println()
			fmt.Printf("  Compose file: %s\n", composePath)
			fmt.Println()

			// Show current containers
			listCmd := exec.Command("docker", "compose", "ps", "--format", "table {{.Name}}\t{{.Status}}")
			listCmd.Dir = composeDir
			listOut, _ := listCmd.Output()
			if len(listOut) > 0 {
				fmt.Println("  Running containers:")
				for _, line := range strings.Split(strings.TrimSpace(string(listOut)), "\n") {
					fmt.Printf("    %s\n", line)
				}
				fmt.Println()
			}

			action := "Stop + remove all containers"
			if removeVolumes {
				action += " AND volumes (⚠ database data will be lost)"
			} else {
				action += " (volumes/data preserved)"
			}
			fmt.Printf("  Action: %s\n", output.Bold(action))
			fmt.Println()

			// Confirm unless --force
			if !forceFlag && !flagAgent {
				reader := bufio.NewReader(os.Stdin)
				prompt := "  Continue? [y/N]: "
				if removeVolumes {
					prompt = fmt.Sprintf("  %s Type 'yes' to confirm: ", output.Red("⚠ This will delete all data."))
				}
				fmt.Print(prompt)
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if removeVolumes {
					if answer != "yes" {
						fmt.Println()
						output.F.Info("Teardown cancelled.")
						return nil
					}
				} else {
					if answer != "y" && answer != "yes" {
						fmt.Println()
						output.F.Info("Teardown cancelled.")
						return nil
					}
				}
			}

			fmt.Println()

			// Execute teardown
			downArgs := []string{"compose", "down"}
			if removeVolumes {
				downArgs = append(downArgs, "-v")
			}
			downArgs = append(downArgs, "--remove-orphans")

			fmt.Print("  Tearing down... ")
			downCmd := exec.Command("docker", downArgs...)
			downCmd.Dir = composeDir
			downOut, err := downCmd.CombinedOutput()
			if err != nil {
				fmt.Println(output.Red("failed"))
				fmt.Printf("\n%s\n", string(downOut))
				return fmt.Errorf("docker compose down failed: %w", err)
			}
			// Clean up containers from old "temporal" project name (migration)
			bootstrap.CleanupOldProjectContainers()
			fmt.Println(output.Green("done"))

			fmt.Println()
			if removeVolumes {
				output.Successf("All containers and volumes removed.")
			} else {
				output.Successf("All containers removed. Data volumes preserved.")
			}
			output.F.Info("Run 'reposwarm new --local' to start fresh.")

			return nil
		},
	}

	cmd.Flags().BoolVar(&forceFlag, "force", false, "Skip confirmation prompt")
	cmd.Flags().BoolVarP(&removeVolumes, "volumes", "v", false, "Also remove Docker volumes (deletes all data)")
	return cmd
}
