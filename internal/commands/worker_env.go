package commands

import (
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reposwarm/reposwarm-cli/internal/bootstrap"
	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newConfigWorkerEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "worker-env",
		Aliases: []string{"env"},
		Short:   "Manage worker environment variables",
	}
	cmd.AddCommand(newWorkerEnvListCmd())
	cmd.AddCommand(newWorkerEnvSetCmd())
	cmd.AddCommand(newWorkerEnvUnsetCmd())
	return cmd
}

// workerEnvFilePath returns the path to the worker.env file, or empty string if not found.
func workerEnvFilePath() string {
	cfg, err := config.Load()
	if err != nil {
		return ""
	}
	installDir := cfg.EffectiveInstallDir()
	p := filepath.Join(installDir, bootstrap.ComposeSubDir, "worker.env")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func newWorkerEnvListCmd() *cobra.Command {
	var reveal bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show all worker environment variables",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Try API first
			client, clientErr := getClient()
			if clientErr == nil {
				path := "/workers/worker-1/env"
				if reveal {
					path += "?reveal=true"
				}

				var resp struct {
					EnvFile string `json:"envFile"`
					Entries []struct {
						Key    string `json:"key"`
						Value  string `json:"value"`
						Source string `json:"source"`
						Set    bool   `json:"set"`
					} `json:"entries"`
				}
				if err := client.Get(ctx(), path, &resp); err == nil {
					// Overlay worker.env file values on top of API response
					// API may not see vars written directly to worker.env
					fileVars := map[string]string{}
					cfg, cfgErr := config.Load()
					if cfgErr == nil {
						if fv, fErr := bootstrap.ReadWorkerEnvFile(cfg.EffectiveInstallDir()); fErr == nil {
							fileVars = fv
						}
					}

					// Update entries that the API thinks are unset but file has them
					for i := range resp.Entries {
						if !resp.Entries[i].Set {
							if fv, ok := fileVars[resp.Entries[i].Key]; ok && fv != "" {
								resp.Entries[i].Set = true
								if reveal {
									resp.Entries[i].Value = fv
								} else {
									resp.Entries[i].Value = config.MaskedToken(fv)
								}
								resp.Entries[i].Source = "file"
							}
						}
					}
					// Add file-only vars not in API response
					apiKeys := map[string]bool{}
					for _, e := range resp.Entries {
						apiKeys[e.Key] = true
					}
					for k, v := range fileVars {
						if !apiKeys[k] && v != "" {
							val := v
							if !reveal {
								val = config.MaskedToken(v)
							}
							resp.Entries = append(resp.Entries, struct {
								Key    string `json:"key"`
								Value  string `json:"value"`
								Source string `json:"source"`
								Set    bool   `json:"set"`
							}{k, val, "file", true})
						}
					}

					if flagJSON {
						return output.JSON(resp)
					}

					F := output.F
					F.Section("Worker Environment")
					F.KeyValue("Env file", resp.EnvFile)
					F.Println()

					headers := []string{"Variable", "Value", "Source"}
					var rows [][]string
					for _, e := range resp.Entries {
						valStr := e.Value
						if !e.Set {
							valStr = output.Dim("(not set)")
						} else if !reveal {
							// Mask all values consistently unless --reveal
							valStr = config.MaskedToken(valStr)
						}
						rows = append(rows, []string{e.Key, valStr, e.Source})
					}
					output.Table(headers, rows)
					F.Println()
					return nil
				}
			}

			// Fallback: read worker.env file directly
			envPath := workerEnvFilePath()
			if envPath == "" {
				return fmt.Errorf("API unreachable and no worker.env file found.\nSet up locally: reposwarm new --local")
			}

			cfg, _ := config.Load()
			envVars, err := bootstrap.ReadWorkerEnvFile(cfg.EffectiveInstallDir())
			if err != nil {
				return fmt.Errorf("reading worker.env: %w", err)
			}

			if flagJSON {
				entries := []map[string]any{}
				for k, v := range envVars {
					val := v
					if !reveal {
						val = config.MaskedToken(v)
					}
					entries = append(entries, map[string]any{"key": k, "value": val, "set": true, "source": "file"})
				}
				return output.JSON(map[string]any{"envFile": envPath, "entries": entries, "source": "file"})
			}

			if !flagJSON {
				output.F.Warning("API unreachable — reading from local file")
			}
			F := output.F
			F.Section("Worker Environment (from file)")
			F.KeyValue("Env file", envPath)
			F.Println()

			keys := make([]string, 0, len(envVars))
			for k := range envVars {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			headers := []string{"Variable", "Value", "Source"}
			var rows [][]string
			for _, k := range keys {
				v := envVars[k]
				if !reveal {
					v = config.MaskedToken(v)
				}
				rows = append(rows, []string{k, v, "file"})
			}
			output.Table(headers, rows)
			F.Println()
			return nil
		},
	}

	cmd.Flags().BoolVar(&reveal, "reveal", false, "Show full unmasked values")
	return cmd
}

func newWorkerEnvSetCmd() *cobra.Command {
	var restart bool

	cmd := &cobra.Command{
		Use:   "set <KEY> <VALUE>",
		Short: "Set a worker environment variable",
		Args:  friendlyExactArgs(2, "reposwarm config worker-env set <KEY> <VALUE>\n\nExample:\n  reposwarm config worker-env set ANTHROPIC_API_KEY sk-ant-abc123"),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := args[0], args[1]

			// For Docker installs, always write directly to worker.env
			// The API's /workers/worker-1/env endpoint may not have write access
			// to the Docker volume, making direct file write more reliable
			envPath := workerEnvFilePath()
			if envPath != "" {
				cfg, _ := config.Load()
				envVars, _ := bootstrap.ReadWorkerEnvFile(cfg.EffectiveInstallDir())
				if envVars == nil {
					envVars = make(map[string]string)
				}
				envVars[key] = value

				if err := writeWorkerEnvMap(envPath, envVars); err != nil {
					return fmt.Errorf("writing worker.env: %w", err)
				}

				// Also try to sync via API (best-effort, don't fail if it errors)
				if client, clientErr := getClient(); clientErr == nil {
					body := map[string]string{"value": value}
					var resp any
					_ = client.Put(ctx(), "/workers/worker-1/env/"+key, body, &resp)
				}

				if flagJSON {
					return output.JSON(map[string]any{
						"key": key, "value": config.MaskedToken(value),
						"envFile": envPath, "restart": restart,
					})
				}

				output.Successf("Set %s = %s (written to %s)", key, config.MaskedToken(value), envPath)

				if restart {
					cfgR, cfgErr := config.Load()
					if cfgErr == nil && bootstrap.IsDockerInstall(cfgR.EffectiveInstallDir()) {
						composeDir := filepath.Join(cfgR.EffectiveInstallDir(), bootstrap.ComposeSubDir)
						output.F.Info("Restarting worker...")
						// Stop first to avoid container rename collision
						stopCmd := osexec.Command("docker", "compose", "stop", "worker")
						stopCmd.Dir = composeDir
						stopCmd.CombinedOutput() // ignore error if already stopped
						restartCmd := osexec.Command("docker", "compose", "up", "-d", "--force-recreate", "worker")
						restartCmd.Dir = composeDir
						if out, err := restartCmd.CombinedOutput(); err != nil {
							output.F.Warning(fmt.Sprintf("Could not restart: %v (%s)", err, string(out)))
						} else {
							output.Successf("Worker restarted")
						}
					} else {
						output.F.Warning("Worker restart required. Run: reposwarm restart worker")
					}
				} else {
					output.F.Warning("Worker restart required. Run: reposwarm restart worker")
				}
				return nil
			}

			// No worker.env file — try API only
			client, clientErr := getClient()
			if clientErr == nil {
				body := map[string]string{"value": value}
				var resp struct {
					Key     string `json:"key"`
					Value   string `json:"value"`
					EnvFile string `json:"envFile"`
				}
				if err := client.Put(ctx(), "/workers/worker-1/env/"+key, body, &resp); err == nil {
					if flagJSON {
						return output.JSON(map[string]any{
							"key": resp.Key, "value": resp.Value,
							"envFile": resp.EnvFile, "restart": restart,
						})
					}
					output.Successf("Set %s = %s (written to %s)", key, resp.Value, resp.EnvFile)

					if restart {
						output.F.Info("Restarting worker...")
						var restartResp any
						if err := client.Post(ctx(), "/workers/worker-1/restart", nil, &restartResp); err != nil {
							output.F.Warning(fmt.Sprintf("Could not restart: %v", err))
							output.F.Info("Restart manually: reposwarm restart worker")
						} else {
							output.Successf("Worker restarted")
						}
					} else {
						output.F.Warning("Worker restart required. Run: reposwarm restart worker")
					}
					return nil
				}
			}

			return fmt.Errorf("no worker.env file found and API unavailable.\nSet up locally: reposwarm new --local")
		},
	}

	cmd.Flags().BoolVar(&restart, "restart", false, "Automatically restart the worker after setting")
	return cmd
}

func newWorkerEnvUnsetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unset <KEY>",
		Short: "Remove a worker environment variable",
		Args:  friendlyExactArgs(1, "reposwarm config worker-env unset <KEY>\n\nExample:\n  reposwarm config worker-env unset SOME_VAR"),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			// Try API first
			client, clientErr := getClient()
			if clientErr == nil {
				var resp any
				if err := client.Delete(ctx(), "/workers/worker-1/env/"+key, &resp); err == nil {
					// Also remove from host worker.env
					if ep := workerEnvFilePath(); ep != "" {
						cfg, _ := config.Load()
						if envVars, fErr := bootstrap.ReadWorkerEnvFile(cfg.EffectiveInstallDir()); fErr == nil {
							delete(envVars, key)
							_ = writeWorkerEnvMap(ep, envVars)
						}
					}
					if flagJSON {
						return output.JSON(map[string]any{"key": key, "removed": true})
					}
					output.Successf("Removed %s", key)
					output.F.Warning("Worker restart required. Run: reposwarm restart worker")
					return nil
				}
			}

			// Fallback: remove from worker.env file
			envPath := workerEnvFilePath()
			if envPath == "" {
				return fmt.Errorf("API unreachable and no worker.env file found.\nSet up locally: reposwarm new --local")
			}

			if !flagJSON {
				output.F.Warning("API unreachable — editing local file")
			}

			cfg, _ := config.Load()
			envVars, _ := bootstrap.ReadWorkerEnvFile(cfg.EffectiveInstallDir())
			if envVars == nil {
				envVars = make(map[string]string)
			}
			delete(envVars, key)

			if err := writeWorkerEnvMap(envPath, envVars); err != nil {
				return fmt.Errorf("writing worker.env: %w", err)
			}

			if flagJSON {
				return output.JSON(map[string]any{"key": key, "removed": true, "source": "file"})
			}

			output.Successf("Removed %s from %s", key, envPath)
			output.F.Warning("Worker restart required. Run: reposwarm restart worker")
			return nil
		},
	}
	return cmd
}

// writeWorkerEnvMap writes a map of env vars to a worker.env file.
func writeWorkerEnvMap(path string, vars map[string]string) error {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", k, vars[k]))
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}
