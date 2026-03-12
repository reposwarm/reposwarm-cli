package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reposwarm/reposwarm-cli/internal/bootstrap"
	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

// newConfigArchHubCmd creates the "config arch-hub" subcommand tree.
func newConfigArchHubCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "arch-hub",
		Short: "Configure architecture hub storage (local directory or GitHub repo)",
		Long: `Switch between local file storage and GitHub repo modes for architecture
hub output. Local mode stores .arch.md files on disk; GitHub mode pushes
them to a git repository.

Examples:
  reposwarm config arch-hub show
  reposwarm config arch-hub local ~/my-arch-hub
  reposwarm config arch-hub github --url https://github.com/my-org --repo arch-hub`,
	}

	cmd.AddCommand(newArchHubLocalCmd())
	cmd.AddCommand(newArchHubGitHubCmd())
	cmd.AddCommand(newArchHubShowCmd())
	return cmd
}

// newArchHubLocalCmd creates the "config arch-hub local <path>" command.
func newArchHubLocalCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "local <path>",
		Short: "Switch to local file storage mode",
		Long: `Store architecture hub output in a local directory. The path is mounted
into the worker container at /data/arch-hub.

Example:
  reposwarm config arch-hub local ~/my-arch-hub`,
		Args: friendlyExactArgs(1, "reposwarm config arch-hub local <path>\n\nExample:\n  reposwarm config arch-hub local ~/my-arch-hub"),
		RunE: func(cmd *cobra.Command, args []string) error {
			rawPath := args[0]

			// Resolve to absolute path
			absPath, err := filepath.Abs(rawPath)
			if err != nil {
				return fmt.Errorf("resolving path: %w", err)
			}

			// Create directory if it doesn't exist
			if err := os.MkdirAll(absPath, 0755); err != nil {
				return fmt.Errorf("creating directory %s: %w", absPath, err)
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			installDir := cfg.EffectiveInstallDir()

			// Set worker env vars
			envVars := map[string]string{
				"ARCH_HUB_MODE":       "local",
				"ARCH_HUB_LOCAL_PATH": "/data/arch-hub",
			}
			composeDir := filepath.Join(installDir, bootstrap.ComposeSubDir)
			envPath := filepath.Join(composeDir, "worker.env")
			if err := mergeWorkerEnvVars(envPath, envVars); err != nil {
				return fmt.Errorf("updating worker.env: %w", err)
			}

			// Update docker-compose.yml with bind mount
			if err := bootstrap.UpdateComposeWorkerMount(installDir, absPath, "/data/arch-hub"); err != nil {
				return fmt.Errorf("updating docker-compose.yml: %w", err)
			}

			// Recreate worker container
			if err := recreateWorker(composeDir); err != nil {
				return fmt.Errorf("recreating worker: %w", err)
			}

			if flagJSON {
				return output.JSON(map[string]any{
					"mode":          "local",
					"hostPath":      absPath,
					"containerPath": "/data/arch-hub",
				})
			}

			output.Successf("Arch-hub switched to local mode")
			output.F.KeyValue("hostPath", absPath)
			output.F.KeyValue("containerPath", "/data/arch-hub")
			return nil
		},
	}
}

// newArchHubGitHubCmd creates the "config arch-hub github" command.
func newArchHubGitHubCmd() *cobra.Command {
	var url, repo, token string

	cmd := &cobra.Command{
		Use:   "github",
		Short: "Switch to GitHub repo mode",
		Long: `Store architecture hub output in a GitHub repository. Requires a base URL
and optionally a repo name and GitHub token.

Example:
  reposwarm config arch-hub github --url https://github.com/my-org --repo arch-hub --token ghp_xxx`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if url == "" {
				return fmt.Errorf("--url is required\n\nUsage:\n  reposwarm config arch-hub github --url <base-url> [--repo <name>] [--token <token>]")
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			installDir := cfg.EffectiveInstallDir()

			// Set worker env vars
			envVars := map[string]string{
				"ARCH_HUB_MODE":     "git",
				"ARCH_HUB_BASE_URL": url,
			}
			if repo != "" {
				envVars["ARCH_HUB_REPO_NAME"] = repo
			}
			if token != "" {
				envVars["GITHUB_TOKEN"] = token
			}

			composeDir := filepath.Join(installDir, bootstrap.ComposeSubDir)
			envPath := filepath.Join(composeDir, "worker.env")
			if err := mergeWorkerEnvVars(envPath, envVars); err != nil {
				return fmt.Errorf("updating worker.env: %w", err)
			}

			// Remove any local bind mount
			if err := bootstrap.RemoveComposeWorkerMount(installDir, "/data/arch-hub"); err != nil {
				return fmt.Errorf("updating docker-compose.yml: %w", err)
			}

			// Recreate worker container
			if err := recreateWorker(composeDir); err != nil {
				return fmt.Errorf("recreating worker: %w", err)
			}

			if flagJSON {
				result := map[string]any{
					"mode": "git",
					"url":  url,
				}
				if repo != "" {
					result["repo"] = repo
				}
				if token != "" {
					result["tokenSet"] = true
				}
				return output.JSON(result)
			}

			output.Successf("Arch-hub switched to GitHub mode")
			output.F.KeyValue("baseUrl", url)
			if repo != "" {
				output.F.KeyValue("repoName", repo)
			}
			if token != "" {
				output.F.KeyValue("token", "***set***")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&url, "url", "", "GitHub org/user base URL (e.g. https://github.com/my-org)")
	cmd.Flags().StringVar(&repo, "repo", "", "Architecture hub repo name (default: architecture-hub)")
	cmd.Flags().StringVar(&token, "token", "", "GitHub token for push access")
	return cmd
}

// newArchHubShowCmd creates the "config arch-hub show" command.
func newArchHubShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current arch-hub configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			installDir := cfg.EffectiveInstallDir()

			env, _ := bootstrap.ReadWorkerEnvFile(installDir)
			if env == nil {
				env = make(map[string]string)
			}

			mode := env["ARCH_HUB_MODE"]
			if mode == "" {
				mode = "git"
			}

			if flagJSON {
				result := map[string]any{"mode": mode}
				if mode == "local" {
					result["localPath"] = env["ARCH_HUB_LOCAL_PATH"]
				} else {
					result["baseUrl"] = env["ARCH_HUB_BASE_URL"]
					result["repoName"] = env["ARCH_HUB_REPO_NAME"]
					if env["GITHUB_TOKEN"] != "" {
						result["tokenSet"] = true
					}
				}
				return output.JSON(result)
			}

			output.F.Section("Arch-Hub Configuration")
			output.F.KeyValue("mode", mode)

			if mode == "local" {
				localPath := env["ARCH_HUB_LOCAL_PATH"]
				if localPath == "" {
					localPath = "(not set)"
				}
				output.F.KeyValue("containerPath", localPath)
			} else {
				baseURL := env["ARCH_HUB_BASE_URL"]
				if baseURL == "" {
					baseURL = "(not set)"
				}
				output.F.KeyValue("baseUrl", baseURL)

				repoName := env["ARCH_HUB_REPO_NAME"]
				if repoName == "" {
					repoName = "architecture-hub (default)"
				}
				output.F.KeyValue("repoName", repoName)

				if env["GITHUB_TOKEN"] != "" {
					output.F.KeyValue("token", "***set***")
				} else {
					output.F.KeyValue("token", "(not set)")
				}
			}

			return nil
		},
	}
}

// mergeWorkerEnvVars reads an existing worker.env file, merges in new vars, and writes back.
func mergeWorkerEnvVars(path string, vars map[string]string) error {
	existing := make(map[string]string)

	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if i := strings.IndexByte(line, '='); i > 0 {
				existing[line[:i]] = line[i+1:]
			}
		}
	}

	for k, v := range vars {
		existing[k] = v
	}

	var keys []string
	for k := range existing {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", k, existing[k]))
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}

// recreateWorker force-recreates the worker Docker container.
func recreateWorker(composeDir string) error {
	stopCmd := exec.Command("docker", "compose", "stop", "worker")
	stopCmd.Dir = composeDir
	_, _ = stopCmd.CombinedOutput()

	upCmd := exec.Command("docker", "compose", "up", "-d", "--force-recreate", "worker")
	upCmd.Dir = composeDir
	if out, err := upCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose up failed: %w\n%s", err, string(out))
	}
	return nil
}
