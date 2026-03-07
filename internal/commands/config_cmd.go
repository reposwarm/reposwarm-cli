package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/reposwarm/reposwarm-cli/internal/api"
	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
	}
	cmd.AddCommand(newConfigInitCmd())
	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigWorkerEnvCmd())
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigServerCmd())
	cmd.AddCommand(newConfigServerSetCmd())
	cmd.AddCommand(newConfigProviderCmd())
	cmd.AddCommand(newConfigModelCmd())
	cmd.AddCommand(newConfigGitCmd())
	return cmd
}

func newConfigInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive setup wizard",
		Long:  "Set up API URL and token interactively. Tests the connection before saving.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.DefaultConfig()
			reader := bufio.NewReader(os.Stdin)

			F := output.F
			F.Section("RepoSwarm CLI Setup")

			fmt.Printf("  API URL [%s]: ", cfg.APIUrl)
			if line, _ := reader.ReadString('\n'); strings.TrimSpace(line) != "" {
				cfg.APIUrl = strings.TrimSpace(line)
			}

			for cfg.APIToken == "" {
				fmt.Print("  API Token: ")
				line, _ := reader.ReadString('\n')
				cfg.APIToken = strings.TrimSpace(line)
				if cfg.APIToken == "" {
					F.Warning("API token is required — paste your token and press Enter")
				}
			}

			F.Info(fmt.Sprintf("Testing connection to %s...", cfg.APIUrl))
			client := api.New(cfg.APIUrl, cfg.APIToken)
			health, err := client.Health(ctx())
			if err != nil {
				return fmt.Errorf("connection test failed: %w", err)
			}

			F.Success(fmt.Sprintf("Connected to RepoSwarm API %s (%s)", health.Version, health.Status))

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			path, _ := config.ConfigPath()
			F.Success(fmt.Sprintf("Config saved to %s", path))
			F.Println()
			return nil
		},
	}
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Display current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if flagJSON {
				display := map[string]any{
					"apiUrl":       cfg.APIUrl,
					"apiToken":     config.MaskedToken(cfg.APIToken),
					"region":       cfg.Region,
					"defaultModel": cfg.DefaultModel,
					"chunkSize":    cfg.ChunkSize,
					"outputFormat": cfg.OutputFormat,
					"installDir":   cfg.EffectiveInstallDir(),
					"provider":     cfg.ProviderConfig.Provider,
					"authMethod":   cfg.ProviderConfig.BedrockAuth,
					"gitProvider":  cfg.GitProvider,
				}
				// Try to include server config
				client, clientErr := getClient()
				if clientErr == nil {
					var serverCfg api.ConfigResponse
					if err := client.Get(ctx(), "/config", &serverCfg); err == nil {
						display["server"] = serverCfg
						if cfg.EffectiveModel() != serverCfg.DefaultModel && serverCfg.DefaultModel != "" {
							display["modelDrift"] = true
						}
					}
				}
				return output.JSON(display)
			}

			F := output.F
			F.Section("RepoSwarm CLI Configuration")
			F.KeyValue("apiUrl", cfg.APIUrl)
			F.KeyValue("apiToken", config.MaskedToken(cfg.APIToken))
			F.KeyValue("region", cfg.Region)
			F.KeyValue("defaultModel", cfg.DefaultModel)
			F.KeyValue("chunkSize", fmt.Sprint(cfg.ChunkSize))
			F.KeyValue("outputFormat", cfg.OutputFormat)
			F.KeyValue("installDir", cfg.EffectiveInstallDir())
			F.Println()

			// Provider config
			F.Section("LLM Provider")
			provider := string(cfg.EffectiveProvider())
			if cfg.ProviderConfig.Provider == "" && cfg.DefaultModel == "" {
				// Nothing configured at all
				F.KeyValue("provider", "(not configured)")
				F.Info("Run: reposwarm config provider setup")
			} else {
				F.KeyValue("provider", provider)
			}
			if cfg.ProviderConfig.Provider == config.ProviderBedrock {
				auth := string(cfg.ProviderConfig.BedrockAuth)
				if auth == "" {
					auth = "iam-role (default)"
				}
				F.KeyValue("authMethod", auth)
				if cfg.ProviderConfig.AWSRegion != "" {
					F.KeyValue("awsRegion", cfg.ProviderConfig.AWSRegion)
				}
			}
			if cfg.ProviderConfig.Provider == config.ProviderLiteLLM {
				if cfg.ProviderConfig.ProxyURL != "" {
					F.KeyValue("proxyUrl", cfg.ProviderConfig.ProxyURL)
				}
			}
			if cfg.DefaultModel != "" {
				F.KeyValue("model", cfg.DefaultModel)
			}
			if cfg.ProviderConfig.SmallModel != "" {
				F.KeyValue("smallModel", cfg.ProviderConfig.SmallModel)
			}
			F.Println()

			// Git provider config
			F.Section("Git Provider")
			gitProvider := cfg.GitProvider
			if gitProvider == "" {
				F.KeyValue("provider", "(not configured)")
				F.Info("Run: reposwarm config git setup")
			} else {
				b, _ := config.GetGitProviderBundle(gitProvider)
				label := gitProvider
				if b != nil {
					label = fmt.Sprintf("%s (%s)", b.Label, gitProvider)
				}
				F.KeyValue("provider", label)
			}
			F.Println()

			// Show server-side config if API is reachable
			client, err := getClient()
			if err == nil {
				var serverCfg api.ConfigResponse
				if err := client.Get(ctx(), "/config", &serverCfg); err == nil {
					F.Section("Server Configuration")
					F.KeyValue("defaultModel", serverCfg.DefaultModel)
					F.KeyValue("chunkSize", fmt.Sprint(serverCfg.ChunkSize))
					F.KeyValue("parallelLimit", fmt.Sprint(serverCfg.ParallelLimit))

					// Config drift warning — skip if models are aliases of the same thing
					if cfg.EffectiveModel() != serverCfg.DefaultModel && serverCfg.DefaultModel != "" {
						// Check if one model ID is a versioned form of the other
						// e.g. "us.anthropic.claude-sonnet-4-6" vs "us.anthropic.claude-sonnet-4-20250514-v1:0"
						cliModel := cfg.EffectiveModel()
						srvModel := serverCfg.DefaultModel
						sameFamily := false
						for _, a := range config.KnownAliases() {
							if (cliModel == a.Bedrock || cliModel == a.Anthropic) &&
								(srvModel == a.Bedrock || srvModel == a.Anthropic) {
								sameFamily = true
								break
							}
						}
						// Also check substring match (e.g. both contain "claude-sonnet-4")
						if !sameFamily {
							// Extract the model family name (strip version suffixes)
							cliBase := strings.Split(strings.TrimPrefix(cliModel, "us."), "-v")[0]
							srvBase := strings.Split(strings.TrimPrefix(srvModel, "us."), "-v")[0]
							if strings.HasPrefix(cliBase, srvBase) || strings.HasPrefix(srvBase, cliBase) {
								sameFamily = true
							}
						}
						if !sameFamily {
							F.Println()
							F.Warning(fmt.Sprintf("Model drift: CLI default '%s' ≠ server '%s'", cliModel, srvModel))
						}
					}
					F.Println()
				}
			}

			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  friendlyExactArgs(2, "reposwarm config set <key> <value>\n\nExample:\n  reposwarm config set apiUrl http://localhost:3000"),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if err := config.Set(cfg, args[0], args[1]); err != nil {
				return err
			}

			if err := config.Save(cfg); err != nil {
				return err
			}

			output.F.Success(fmt.Sprintf("Set %s = %s", args[0], args[1]))
			return nil
		},
	}
}
