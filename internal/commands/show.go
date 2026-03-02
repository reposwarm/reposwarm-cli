package commands

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/loki-bedlam/reposwarm-cli/internal/config"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <target>",
		Short: "Open a URL in the default browser",
		Long: `Open a URL in the default browser for the specified target.

Valid targets:
  temporal  — Temporal UI
  ui        — RepoSwarm UI
  api       — API health endpoint
  hub       — GitHub repository for the project`,
		Args: friendlyExactArgs(1, "reposwarm show <target>\n\nTargets: temporal, ui, api, hub\n\nExample:\n  reposwarm show temporal"),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			url, err := getURLForTarget(cfg, target)
			if err != nil {
				return err
			}

			// For agent mode: just print URL and message
			if flagAgent {
				fmt.Printf("%s\n(agent mode: URL not opened)\n", url)
				return nil
			}

			// JSON output
			if flagJSON {
				opened := false
				if !flagAgent {
					opened = openBrowser(url) == nil
				}
				return output.JSON(map[string]any{
					"target": target,
					"url":    url,
					"opened": opened,
				})
			}

			// Human-friendly output
			F := output.F
			if err := openBrowser(url); err != nil {
				F.Error(fmt.Sprintf("Failed to open browser: %s", err))
				F.Info(fmt.Sprintf("URL: %s", url))
				return nil
			}

			F.Success(fmt.Sprintf("Opened %s in browser: %s", target, url))
			return nil
		},
	}
}

// getURLForTarget returns the URL for the specified target.
func getURLForTarget(cfg *config.Config, target string) (string, error) {
	switch target {
	case "temporal":
		return fmt.Sprintf("http://localhost:%s", cfg.EffectiveTemporalUIPort()), nil
	case "ui":
		return fmt.Sprintf("http://localhost:%s", cfg.EffectiveUIPort()), nil
	case "api":
		// Use configured API URL if set, otherwise construct from port
		if cfg.APIUrl != "" {
			return cfg.APIUrl + "/health", nil
		}
		return fmt.Sprintf("http://localhost:%s/health", cfg.EffectiveAPIPort()), nil
	case "hub":
		return cfg.EffectiveHubURL(), nil
	default:
		return "", fmt.Errorf("unknown target: %s (valid: temporal, ui, api, hub)", target)
	}
}

// openBrowser opens the specified URL in the default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}
