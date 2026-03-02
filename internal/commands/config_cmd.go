package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/config"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
	}
	cmd.AddCommand(newConfigInitCmd())
	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigServerCmd())
	cmd.AddCommand(newConfigServerSetCmd())
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
			F.Println()
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
