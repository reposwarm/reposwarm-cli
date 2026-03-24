package commands

import (
	"fmt"
	"time"

	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check API health and connection",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			start := time.Now()
			health, err := client.Health(ctx())
			latency := time.Since(start)

			if err != nil {
				if flagJSON {
					return output.JSON(map[string]any{
						"connected": false,
						"error":     err.Error(),
					})
				}
				output.F.Error(fmt.Sprintf("Connection failed: %s", err))
				return nil
			}

			cfg, _ := config.Load()

			if flagJSON {
				result := map[string]any{
					"connected": true,
					"status":    health.Status,
					"version":   health.Version,
					"latency":   latency.Milliseconds(),
					"temporal":  health.Temporal.Connected,
					"dynamodb":  health.DynamoDB.Connected,
					"worker":    health.Worker.Connected,
					"apiUrl":    cfg.APIUrl,
				}
				if cfg.IsDockerInstall() {
					result["uiUrl"] = fmt.Sprintf("http://localhost:%s", cfg.EffectiveUIPort())
					result["temporalUiUrl"] = fmt.Sprintf("http://localhost:%s", cfg.EffectiveTemporalUIPort())
				}
				return output.JSON(result)
			}

			F := output.F
			F.Section("RepoSwarm Status")
			F.KeyValue("API URL", cfg.APIUrl)
			F.KeyValue("Status", health.Status)
			F.KeyValue("Version", health.Version)
			F.KeyValue("Latency", fmt.Sprintf("%dms", latency.Milliseconds()))

			svcStatus := func(name string, connected bool) string {
				if connected {
					return "ok"
				}
				return "DISCONNECTED"
			}

			F.Println()
			F.KeyValue("Temporal", svcStatus("Temporal", health.Temporal.Connected))
			F.KeyValue("DynamoDB", svcStatus("DynamoDB", health.DynamoDB.Connected))
			F.KeyValue("Worker", svcStatus("Worker", health.Worker.Connected))

			if health.Temporal.Connected {
				F.KeyValue("  namespace", health.Temporal.Namespace)
				F.KeyValue("  taskQueue", health.Temporal.TaskQueue)
			}
			if health.Worker.Connected {
				F.KeyValue("  workers", fmt.Sprint(health.Worker.Count))
			}

			// Show service URLs for Docker installs
			if cfg.IsDockerInstall() {
				F.Println()
				F.Section("Service URLs")
				F.KeyValue("Dashboard (UI)", fmt.Sprintf("http://localhost:%s", cfg.EffectiveUIPort()))
				F.KeyValue("API Server", fmt.Sprintf("http://localhost:%s", cfg.EffectiveAPIPort()))
				F.KeyValue("Temporal UI", fmt.Sprintf("http://localhost:%s", cfg.EffectiveTemporalUIPort()))
			}

			F.Println()
			return nil
		},
	}
}
