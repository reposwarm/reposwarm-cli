package commands

import (
	"fmt"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newConfigServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Show server configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			var cfg api.ConfigResponse
			if err := client.Get(ctx(), "/config", &cfg); err != nil {
				return err
			}

			if flagJSON {
				return output.JSON(cfg)
			}

			F := output.F
			F.Section("Server Configuration")
			F.KeyValue("defaultModel", cfg.DefaultModel)
			F.KeyValue("chunkSize", fmt.Sprint(cfg.ChunkSize))
			F.KeyValue("sleepDuration", fmt.Sprintf("%dms", cfg.SleepDuration))
			F.KeyValue("parallelLimit", fmt.Sprint(cfg.ParallelLimit))
			F.KeyValue("tokenLimit", fmt.Sprint(cfg.TokenLimit))
			F.KeyValue("scheduleExpression", cfg.ScheduleExpression)
			F.Println()
			return nil
		},
	}
}

func newConfigServerSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "server-set <key> <value>",
		Short: "Update a server configuration value",
		Args:  friendlyExactArgs(2, "reposwarm config server-set <key> <value>\n\nExample:\n  reposwarm config server-set defaultModel claude-opus-4-6"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			body := map[string]any{args[0]: args[1]}
			var result any
			if err := client.Patch(ctx(), "/config", body, &result); err != nil {
				return err
			}

			if flagJSON {
				return output.JSON(map[string]any{"key": args[0], "value": args[1]})
			}
			output.F.Success(fmt.Sprintf("Set server %s = %s", args[0], args[1]))
			return nil
		},
	}
}
