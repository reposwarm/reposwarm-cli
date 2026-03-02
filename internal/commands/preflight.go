package commands

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

type preflightCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "ok", "fail", "warn"
	Message string `json:"message"`
}

func newPreflightCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preflight [repo]",
		Short: "Verify system readiness for an investigation",
		Long: `Run pre-flight checks without triggering an investigation.
Verifies API, Temporal, workers, environment, and optionally repo access.

Examples:
  reposwarm preflight              # System readiness check
  reposwarm preflight is-odd       # Check system + repo accessibility`,
		Args: friendlyMaxArgs(1, "reposwarm preflight [repo]\n\nExample:\n  reposwarm preflight is-odd"),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := ""
			if len(args) > 0 {
				repo = args[0]
			}

			checks := runPreflightChecks(repo)

			if flagJSON {
				ok, warn, fail := 0, 0, 0
				for _, c := range checks {
					switch c.Status {
					case "ok":
						ok++
					case "warn":
						warn++
					case "fail":
						fail++
					}
				}
				return output.JSON(map[string]any{
					"checks": checks,
					"ready":  fail == 0,
					"ok":     ok,
					"warn":   warn,
					"fail":   fail,
					"repo":   repo,
				})
			}

			F := output.F
			title := "Pre-Flight Check: system"
			if repo != "" {
				title = fmt.Sprintf("Pre-Flight Check: %s", repo)
			}
			F.Section(title)

			fail := 0
			for _, c := range checks {
				switch c.Status {
				case "ok":
					F.Printf("  %s %s: %s\n", output.Green("[OK]"), c.Name, c.Message)
				case "warn":
					F.Printf("  %s %s: %s\n", output.Yellow("[WARN]"), c.Name, c.Message)
				case "fail":
					F.Printf("  %s %s: %s\n", output.Red("[FAIL]"), c.Name, c.Message)
					fail++
				}
			}

			F.Println()
			if fail > 0 {
				F.Error(fmt.Sprintf("System not ready — %d issue(s) to resolve", fail))
			} else {
				F.Success("Ready to investigate ✅")
			}
			return nil
		},
	}

	return cmd
}

func runPreflightChecks(repo string) []preflightCheck {
	var checks []preflightCheck

	// 1. API health
	client, err := getClient()
	if err != nil {
		checks = append(checks, preflightCheck{"API", "fail", fmt.Sprintf("cannot create client: %s", err)})
		return checks
	}

	start := time.Now()
	health, err := client.Health(ctx())
	latency := time.Since(start)
	if err != nil {
		checks = append(checks, preflightCheck{"API", "fail", fmt.Sprintf("unreachable: %s", err)})
		return checks
	}
	checks = append(checks, preflightCheck{"API", "ok", fmt.Sprintf("healthy (%dms)", latency.Milliseconds())})

	// 2. Temporal
	if health.Temporal.Connected {
		checks = append(checks, preflightCheck{"Temporal", "ok", "connected"})
	} else {
		checks = append(checks, preflightCheck{"Temporal", "fail", "not connected"})
	}

	// 3. Workers
	workers := gatherWorkerInfo(client)
	healthy := 0
	total := len(workers)
	for _, w := range workers {
		if w.Status == "healthy" {
			healthy++
		}
	}

	if healthy > 0 {
		checks = append(checks, preflightCheck{
			"Workers",
			"ok",
			fmt.Sprintf("%d healthy on %s", healthy, health.Temporal.TaskQueue),
		})
	} else if total > 0 {
		var reasons []string
		for _, w := range workers {
			if w.Status != "healthy" {
				reasons = append(reasons, fmt.Sprintf("%s: %s", w.Name, w.Status))
			}
		}
		checks = append(checks, preflightCheck{
			"Workers",
			"fail",
			fmt.Sprintf("0 of %d healthy on %s\n       %s", total, health.Temporal.TaskQueue, strings.Join(reasons, "\n       ")),
		})
	} else {
		checks = append(checks, preflightCheck{"Workers", "fail", "no workers detected"})
	}

	// 4. Worker env
	envChecks := getWorkerEnvChecks()
	var missingEnv []string
	for _, ec := range envChecks {
		if !ec.found && ec.name != "AWS_ACCESS_KEY_ID" && ec.name != "AWS_SECRET_ACCESS_KEY" {
			missingEnv = append(missingEnv, ec.name)
		}
	}
	if len(missingEnv) > 0 {
		checks = append(checks, preflightCheck{
			"Worker env",
			"fail",
			fmt.Sprintf("missing: %s", strings.Join(missingEnv, ", ")),
		})
	} else {
		checks = append(checks, preflightCheck{"Worker env", "ok", "all required vars set"})
	}

	// 5. Repo access (if specified)
	if repo != "" {
		repoCheck := checkRepoAccess(repo)
		checks = append(checks, repoCheck)
	}

	// 6. Model (from server config)
	var serverCfg struct {
		DefaultModel string `json:"defaultModel"`
	}
	if err := client.Get(ctx(), "/config", &serverCfg); err == nil && serverCfg.DefaultModel != "" {
		checks = append(checks, preflightCheck{"Model", "ok", serverCfg.DefaultModel})
	} else {
		checks = append(checks, preflightCheck{"Model", "warn", "could not determine model from server"})
	}

	return checks
}

func checkRepoAccess(repo string) preflightCheck {
	// Try GitHub first
	urls := []string{
		fmt.Sprintf("https://github.com/%s", repo),
		fmt.Sprintf("https://api.github.com/repos/%s", repo),
	}

	client := &http.Client{Timeout: 5 * time.Second}
	for _, url := range urls {
		req, _ := http.NewRequestWithContext(context.Background(), "HEAD", url, nil)
		if req == nil {
			continue
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 400 {
				return preflightCheck{"Repository", "ok", fmt.Sprintf("accessible (%s)", url)}
			}
		}
	}

	// If repo has no slash, it's just a name — we can't verify it
	if !strings.Contains(repo, "/") {
		return preflightCheck{"Repository", "warn", fmt.Sprintf("'%s' — cannot verify accessibility (no owner/repo format)", repo)}
	}

	return preflightCheck{"Repository", "warn", fmt.Sprintf("could not verify access to '%s'", repo)}
}
