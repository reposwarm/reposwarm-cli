package commands

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/loki-bedlam/reposwarm-cli/internal/config"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newConfigGitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "git",
		Short: "Configure git provider for repository access",
		Long: `Configure which git provider RepoSwarm uses to clone and access repositories.

This determines what authentication credentials are needed (e.g. GitHub token,
GitLab token, AWS CodeCommit IAM role, Azure DevOps PAT, etc.)

Examples:
  reposwarm config git setup           # Interactive setup wizard
  reposwarm config git show            # Show current git provider config
  reposwarm config git set <provider>  # Set git provider directly`,
	}

	cmd.AddCommand(newConfigGitSetupCmd())
	cmd.AddCommand(newConfigGitShowCmd())
	cmd.AddCommand(newConfigGitSetCmd())
	return cmd
}

func newConfigGitSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive git provider setup",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			providers := config.ValidGitProviders()
			sort.Strings(providers)

			if !flagJSON {
				output.F.Section("Git Provider Setup")
				fmt.Println()
				output.F.Info("Choose your git hosting provider:")
				fmt.Println()
			}

			// Show options
			for i, p := range providers {
				b, err := config.GetGitProviderBundle(p)
				if err != nil {
					continue
				}
				marker := "  "
				if p == cfg.GitProvider {
					marker = "→ "
				}
				fmt.Printf("  %s%d) %s (%s)\n", marker, i+1, b.Label, p)
			}
			fmt.Println()

			// Prompt
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("  Select provider (number or name): ")
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(answer)

			// Resolve answer
			selected := ""
			for i, p := range providers {
				if answer == fmt.Sprintf("%d", i+1) || strings.EqualFold(answer, p) {
					selected = p
					break
				}
			}

			if selected == "" {
				return fmt.Errorf("invalid selection: %s", answer)
			}

			return applyGitProvider(cfg, selected, reader)
		},
	}
}

func newConfigGitShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current git provider configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			gitProvider := cfg.GitProvider
			if gitProvider == "" {
				gitProvider = "(not configured)"
			}

			if flagJSON {
				result := map[string]any{"gitProvider": cfg.GitProvider}
				if cfg.GitProvider != "" {
					b, _ := config.GetGitProviderBundle(cfg.GitProvider)
					if b != nil {
						result["label"] = b.Label
						result["envVars"] = b.EnvVars
						result["hint"] = b.Hint
					}
					result["requiredEnvVars"] = config.GitProviderEnvVars(cfg.GitProvider)
				}
				return output.JSON(result)
			}

			if flagAgent {
				fmt.Printf("git_provider: %s\n", gitProvider)
				if cfg.GitProvider != "" {
					for _, ev := range config.GitProviderEnvVars(cfg.GitProvider) {
						req := "optional"
						if ev.Required {
							req = "required"
						}
						fmt.Printf("env: %s (%s) [%s]\n", ev.Key, ev.Desc, req)
					}
				}
				return nil
			}

			output.F.Section("Git Provider")
			fmt.Println()

			if cfg.GitProvider == "" {
				output.F.Warning("No git provider configured")
				output.F.Info("Run: reposwarm config git setup")
				fmt.Println()
				return nil
			}

			b, _ := config.GetGitProviderBundle(cfg.GitProvider)
			label := cfg.GitProvider
			if b != nil {
				label = b.Label
			}

			fmt.Printf("  Provider:  %s (%s)\n", output.Bold(label), cfg.GitProvider)
			fmt.Println()

			if b != nil {
				fmt.Printf("  Required credentials:\n")
				for _, ev := range b.EnvVars {
					req := output.Dim("optional")
					if ev.Required {
						req = output.Yellow("required")
					}
					fmt.Printf("    %s — %s [%s]\n", output.Cyan(ev.Key), ev.Desc, req)
				}
				if b.Hint != "" {
					fmt.Printf("\n  %s %s\n", output.Dim("Hint:"), b.Hint)
				}
				if b.AuthNote != "" {
					fmt.Printf("  %s %s\n", output.Dim("Note:"), b.AuthNote)
				}
			}

			fmt.Println()
			return nil
		},
	}
}

func newConfigGitSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <provider>",
		Short: "Set git provider (github, codecommit, gitlab, azure, bitbucket)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			return applyGitProvider(cfg, args[0], nil)
		},
	}
}

func applyGitProvider(cfg *config.Config, provider string, existingReader *bufio.Reader) error {
	// Validate
	b, err := config.GetGitProviderBundle(provider)
	if err != nil {
		valid := config.ValidGitProviders()
		return fmt.Errorf("unknown git provider '%s' (valid: %s)", provider, strings.Join(valid, ", "))
	}

	cfg.GitProvider = provider
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	if flagJSON {
		return output.JSON(map[string]any{
			"gitProvider": provider,
			"label":       b.Label,
			"saved":       true,
		})
	}

	if flagAgent {
		fmt.Printf("  ✓ Git provider set to %s (%s)\n", b.Label, provider)
		fmt.Println()
		if len(b.EnvVars) > 0 {
			fmt.Println("Required environment variables:")
			for _, ev := range b.EnvVars {
				if ev.Required {
					fmt.Printf("  • %s — %s\n", ev.Key, ev.Desc)
					fmt.Printf("    Set: reposwarm config worker-env set %s <value>\n", ev.Key)
				}
			}
			fmt.Println()
		}
		if b.Hint != "" {
			fmt.Println(b.Hint)
		}
		if b.AuthNote != "" {
			fmt.Println(b.AuthNote)
		}
		return nil
	}

	output.Successf("Git provider set to %s (%s)", b.Label, provider)
	fmt.Println()

	if b.Hint != "" {
		output.F.Info(b.Hint)
	}
	if b.AuthNote != "" {
		output.F.Info(b.AuthNote)
	}

	// Interactive: prompt for required env vars
	if len(b.EnvVars) == 0 {
		return nil
	}

	// Try to get current env var values from API
	currentVals := map[string]string{}
	client, clientErr := getClient()
	if clientErr == nil {
		var envResp struct {
			Entries []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
				Set   bool   `json:"set"`
			} `json:"entries"`
		}
		if err := client.Get(ctx(), "/workers/worker-1/env", &envResp); err == nil {
			for _, e := range envResp.Entries {
				if e.Set {
					currentVals[e.Key] = e.Value
				}
			}
		}
	}

	fmt.Println()
	output.F.Info("Configure required credentials:")
	fmt.Println()

	reader := existingReader
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
	anySet := false

	for _, ev := range b.EnvVars {
		if !ev.Required {
			continue
		}

		current := currentVals[ev.Key]
		prompt := fmt.Sprintf("  %s", ev.Key)
		if ev.Desc != "" {
			prompt += fmt.Sprintf(" (%s)", ev.Desc)
		}

		if current != "" {
			masked := current
			if ev.Secret && len(current) > 8 {
				masked = current[:4] + "..." + current[len(current)-4:]
			}
			prompt += fmt.Sprintf(" [%s]", masked)
		}
		prompt += ": "

		fmt.Print(prompt)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)

		if answer == "" {
			if current != "" {
				fmt.Printf("  %s (keeping current value)\n", output.Dim("→"))
			} else {
				fmt.Printf("  %s skipped — set later: %s\n", output.Yellow("⚠"),
					output.Cyan(fmt.Sprintf("reposwarm config worker-env set %s <value>", ev.Key)))
			}
			continue
		}

		// Set via API
		if client != nil {
			body := map[string]string{"value": answer}
			var resp struct {
				Key string `json:"key"`
			}
			if err := client.Put(ctx(), "/workers/worker-1/env/"+ev.Key, body, &resp); err != nil {
				fmt.Printf("  %s failed to set %s: %v\n", output.Red("✗"), ev.Key, err)
				fmt.Printf("  Set manually: %s\n", output.Cyan(fmt.Sprintf("reposwarm config worker-env set %s %s", ev.Key, answer)))
			} else {
				fmt.Printf("  %s %s set\n", output.Green("✓"), ev.Key)
				anySet = true
			}
		} else {
			fmt.Printf("  %s No API connection — set manually: %s\n", output.Yellow("⚠"),
				output.Cyan(fmt.Sprintf("reposwarm config worker-env set %s %s", ev.Key, answer)))
		}
	}

	// Also prompt for optional vars
	for _, ev := range b.EnvVars {
		if ev.Required {
			continue
		}
		current := currentVals[ev.Key]
		prompt := fmt.Sprintf("  %s", ev.Key)
		if ev.Desc != "" {
			prompt += fmt.Sprintf(" (%s)", ev.Desc)
		}
		prompt += " [optional"
		if current != "" {
			masked := current
			if ev.Secret && len(current) > 8 {
				masked = current[:4] + "..." + current[len(current)-4:]
			}
			prompt += fmt.Sprintf(", current: %s", masked)
		}
		prompt += "]: "

		fmt.Print(prompt)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)

		if answer == "" {
			continue
		}

		if client != nil {
			body := map[string]string{"value": answer}
			var resp struct{ Key string }
			if err := client.Put(ctx(), "/workers/worker-1/env/"+ev.Key, body, &resp); err != nil {
				fmt.Printf("  %s failed to set %s: %v\n", output.Red("✗"), ev.Key, err)
			} else {
				fmt.Printf("  %s %s set\n", output.Green("✓"), ev.Key)
				anySet = true
			}
		}
	}

	if anySet {
		fmt.Println()
		fmt.Print("  Restart worker to apply changes? [Y/n]: ")
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))

		if answer == "" || answer == "y" || answer == "yes" {
			if client != nil {
				var restartResp any
				if err := client.Post(ctx(), "/workers/worker-1/restart", nil, &restartResp); err != nil {
					fmt.Printf("  %s Could not restart: %v\n", output.Yellow("⚠"), err)
					output.F.Info("Restart manually: " + output.Cyan("reposwarm restart worker"))
				} else {
					fmt.Printf("  %s Worker restarting\n", output.Green("✓"))
				}
			} else {
				output.F.Info("No API connection — restart manually: " + output.Cyan("reposwarm restart worker"))
			}
		} else {
			output.F.Info("Remember to restart: " + output.Cyan("reposwarm restart worker"))
		}
	}

	return nil
}
