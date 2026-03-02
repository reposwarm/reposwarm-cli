package commands

import (
	"fmt"

	"github.com/loki-bedlam/reposwarm-cli/internal/config"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newURLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "url <service>",
		Short: "Print the URL for a service",
		Long: `Print the URL for a service or all services.

Valid services:
  temporal       — Temporal UI (HTTP)
  temporal-grpc  — Temporal gRPC endpoint
  ui             — RepoSwarm UI
  api            — RepoSwarm API
  hub            — GitHub repository
  all            — Print all URLs with labels`,
		Args: friendlyExactArgs(1, "reposwarm url <service>\n\nServices: temporal, ui, api, hub, all\n\nExample:\n  reposwarm url temporal"),
		RunE: func(cmd *cobra.Command, args []string) error {
			service := args[0]

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if service == "all" {
				return printAllURLs(cfg)
			}

			url, err := getServiceURL(cfg, service)
			if err != nil {
				return err
			}

			// JSON output
			if flagJSON {
				return output.JSON(map[string]any{
					"service": service,
					"url":     url,
				})
			}

			// Agent mode: plain text, just the URL
			if flagAgent {
				fmt.Println(url)
				return nil
			}

			// Human-friendly output
			F := output.F
			F.Printf("  %s: %s\n", output.Bold(service), url)
			return nil
		},
	}
}

// getServiceURL returns the URL for the specified service.
func getServiceURL(cfg *config.Config, service string) (string, error) {
	switch service {
	case "temporal":
		return fmt.Sprintf("http://localhost:%s", cfg.EffectiveTemporalUIPort()), nil
	case "temporal-grpc":
		return fmt.Sprintf("localhost:%s", cfg.EffectiveTemporalPort()), nil
	case "ui":
		return fmt.Sprintf("http://localhost:%s", cfg.EffectiveUIPort()), nil
	case "api":
		// Use configured API URL if set, otherwise construct from port
		if cfg.APIUrl != "" {
			return cfg.APIUrl, nil
		}
		return fmt.Sprintf("http://localhost:%s", cfg.EffectiveAPIPort()), nil
	case "hub":
		return cfg.EffectiveHubURL(), nil
	default:
		return "", fmt.Errorf("unknown service: %s (valid: temporal, temporal-grpc, ui, api, hub, all)", service)
	}
}

// printAllURLs prints all service URLs.
func printAllURLs(cfg *config.Config) error {
	services := []struct {
		name string
		url  string
	}{
		{"temporal", fmt.Sprintf("http://localhost:%s", cfg.EffectiveTemporalUIPort())},
		{"temporal-grpc", fmt.Sprintf("localhost:%s", cfg.EffectiveTemporalPort())},
		{"ui", fmt.Sprintf("http://localhost:%s", cfg.EffectiveUIPort())},
		{"api", cfg.APIUrl},
		{"hub", cfg.EffectiveHubURL()},
	}

	// If API URL is not configured, construct from port
	if services[3].url == "" {
		services[3].url = fmt.Sprintf("http://localhost:%s", cfg.EffectiveAPIPort())
	}

	// JSON output
	if flagJSON {
		result := make(map[string]string)
		for _, svc := range services {
			result[svc.name] = svc.url
		}
		return output.JSON(result)
	}

	// Agent mode: plain text, one per line
	if flagAgent {
		for _, svc := range services {
			fmt.Printf("%s: %s\n", svc.name, svc.url)
		}
		return nil
	}

	// Human-friendly output
	F := output.F
	F.Section("Service URLs")
	for _, svc := range services {
		F.Printf("  %s\n    %s\n", output.Bold(svc.name), output.Cyan(svc.url))
	}
	F.Println()
	return nil
}
