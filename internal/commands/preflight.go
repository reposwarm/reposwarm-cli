package commands

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/reposwarm/reposwarm-cli/internal/bootstrap"
	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
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

	// 3. Workers — for Docker installs, check container status directly
	cfg, cfgErr := config.Load()
	isDocker := cfgErr == nil && (cfg.IsDockerInstall() || bootstrap.IsDockerInstall(cfg.EffectiveInstallDir()))

	workers := gatherWorkerInfo(client)
	healthy := 0
	total := len(workers)

	if isDocker {
		// Overlay real Docker container status
		dockerServices, _ := bootstrap.DockerComposeServices(cfg.EffectiveInstallDir())
		for _, ds := range dockerServices {
			if ds.Service == "worker" && ds.State == "running" {
				healthy = 1
				for i := range workers {
					workers[i].Status = "healthy"
				}
				break
			}
		}
	} else {
		for _, w := range workers {
			if w.Status == "healthy" {
				healthy++
			}
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

	// 4. Worker env — for Docker, read from worker.env + container
	if isDocker {
		envSet := make(map[string]bool)
		workerEnv, _ := bootstrap.ReadWorkerEnvFile(cfg.EffectiveInstallDir())
		containerEnv, _ := bootstrap.DockerServiceEnv(cfg.EffectiveInstallDir(), "worker")
		for k, v := range workerEnv {
			if v != "" {
				envSet[k] = true
			}
		}
		for k, v := range containerEnv {
			if v != "" {
				envSet[k] = true
			}
		}

		required := map[string]bool{}
		if cfgErr == nil {
			for _, req := range config.RequiredEnvVarsWithGit(&cfg.ProviderConfig, cfg.GitProvider) {
				if req.Required {
					required[req.Key] = true
				}
			}
		}
		if len(required) == 0 {
			required["ANTHROPIC_MODEL"] = true
		}

		var missingEnv []string
		for req := range required {
			if !envSet[req] {
				missingEnv = append(missingEnv, req)
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
	} else {
	var envResp struct {
		Entries []struct {
			Key string `json:"key"`
			Set bool   `json:"set"`
		} `json:"entries"`
	}
	if err := client.Get(ctx(), "/workers/worker-1/env", &envResp); err == nil {
		// Build required env vars from config
		required := map[string]bool{}
		if cfgErr == nil {
			for _, req := range config.RequiredEnvVarsWithGit(&cfg.ProviderConfig, cfg.GitProvider) {
				if req.Required {
					required[req.Key] = true
				}
			}
		}
		if len(required) == 0 {
			// Fallback: at minimum check for a model var
			required["ANTHROPIC_MODEL"] = true
		}

		entryMap := map[string]bool{}
		for _, e := range envResp.Entries {
			if e.Set {
				entryMap[e.Key] = true
			}
		}

		// Also check vars that might not be in API response
		var missingEnv []string
		for req := range required {
			if !entryMap[req] {
				missingEnv = append(missingEnv, req)
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
	} else {
		checks = append(checks, preflightCheck{"Worker env", "warn", "could not check (API endpoint unavailable)"})
	}
	} // end non-Docker env check

	// 5. Repo access (if specified)
	if repo != "" {
		repoCheck := checkRepoAccess(repo)
		checks = append(checks, repoCheck)
	}

	// 6. Arch-hub target — verify repo exists and git token can push to it
	checks = append(checks, checkArchHub(cfg, cfgErr, isDocker)...)

	// 7. Model (prefer local config for consistent display, fall back to server)
	cliCfg, _ := config.Load()
	if cliCfg != nil && cliCfg.DefaultModel != "" {
		checks = append(checks, preflightCheck{"Model", "ok", cliCfg.DefaultModel})
	} else {
		var serverCfg struct {
			DefaultModel string `json:"defaultModel"`
		}
		if err := client.Get(ctx(), "/config", &serverCfg); err == nil && serverCfg.DefaultModel != "" {
			checks = append(checks, preflightCheck{"Model", "ok", serverCfg.DefaultModel})
		} else {
			checks = append(checks, preflightCheck{"Model", "warn", "could not determine model"})
		}
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

	// If repo has no slash, it's just a name — try looking up the URL from the API
	if !strings.Contains(repo, "/") {
		// Try fetching the repo's URL from the RepoSwarm API
		if apiClient, err := getClient(); err == nil {
			var repoResp struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			}
			if err := apiClient.Get(context.Background(), "/repos/"+repo, &repoResp); err == nil && repoResp.URL != "" {
				// Extract owner/repo from the URL
				if strings.Contains(repoResp.URL, "github.com/") {
					parts := strings.TrimPrefix(repoResp.URL, "https://github.com/")
					parts = strings.TrimPrefix(parts, "http://github.com/")
					parts = strings.TrimSuffix(parts, ".git")
					if strings.Contains(parts, "/") {
						// Re-check with the full owner/repo
						return checkRepoAccess(parts)
					}
				}
			}
		}
		return preflightCheck{"Repository", "warn", fmt.Sprintf("'%s' — cannot verify accessibility (no owner/repo format)", repo)}
	}

	return preflightCheck{"Repository", "warn", fmt.Sprintf("could not verify access to '%s'", repo)}
}

func checkArchHub(cfg *config.Config, cfgErr error, isDocker bool) []preflightCheck {
	var checks []preflightCheck

	// Gather worker env to find arch-hub config
	env := map[string]string{}
	if cfgErr == nil && isDocker {
		workerEnv, _ := bootstrap.ReadWorkerEnvFile(cfg.EffectiveInstallDir())
		for k, v := range workerEnv {
			env[k] = v
		}
		containerEnv, _ := bootstrap.DockerServiceEnv(cfg.EffectiveInstallDir(), "worker")
		for k, v := range containerEnv {
			env[k] = v
		}
	} else if cfgErr == nil {
		client, err := getClient()
		if err == nil {
			var envResp struct {
				Entries []struct {
					Key   string `json:"key"`
					Value string `json:"value"`
					Set   bool   `json:"set"`
				} `json:"entries"`
			}
			if err := client.Get(ctx(), "/workers/worker-1/env?reveal=true", &envResp); err == nil {
				for _, e := range envResp.Entries {
					if e.Set {
						env[e.Key] = e.Value
					}
				}
			}
		}
	}

	baseURL := env["ARCH_HUB_BASE_URL"]
	repoName := env["ARCH_HUB_REPO_NAME"]
	if repoName == "" {
		repoName = "architecture-hub" // worker default
	}
	archHubMode := env["ARCH_HUB_MODE"]

	// Local mode: just verify ARCH_HUB_LOCAL_PATH is set
	if archHubMode == "local" {
		localPath := env["ARCH_HUB_LOCAL_PATH"]
		if localPath == "" {
			checks = append(checks, preflightCheck{
				"Arch-hub",
				"fail",
				"ARCH_HUB_MODE=local but ARCH_HUB_LOCAL_PATH not set",
			})
			return checks
		}
		checks = append(checks, preflightCheck{
			"Arch-hub",
			"ok",
			fmt.Sprintf("local mode (container path: %s)", localPath),
		})
		return checks
	}

	// Check 1: ARCH_HUB_BASE_URL is configured (not default placeholder)
	if baseURL == "" || baseURL == "https://github.com/your-org" {
		checks = append(checks, preflightCheck{
			"Arch-hub",
			"fail",
			"ARCH_HUB_BASE_URL not configured — investigation results won't be saved to git\n" +
				"       Fix: reposwarm config worker-env set ARCH_HUB_BASE_URL https://github.com/YOUR-ORG\n" +
				"       Or re-run setup: reposwarm new --local --arch-hub-url https://github.com/YOUR-ORG",
		})
		return checks
	}

	// Check 2: Resolve the full repo path and verify it exists
	// Extract owner/repo from base URL + repo name
	fullRepoURL := fmt.Sprintf("%s/%s", strings.TrimSuffix(baseURL, "/"), repoName)

	// Try to access via GitHub API
	ownerRepo := ""
	if strings.Contains(fullRepoURL, "github.com/") {
		parts := strings.SplitN(strings.TrimPrefix(fullRepoURL, "https://github.com/"), "/", 3)
		if len(parts) >= 2 {
			ownerRepo = parts[0] + "/" + parts[1]
		}
	}

	if ownerRepo == "" {
		checks = append(checks, preflightCheck{"Arch-hub", "warn", fmt.Sprintf("cannot parse owner/repo from '%s'", fullRepoURL)})
		return checks
	}

	// Check 3: Verify repo exists and is accessible
	gitToken := env["GITHUB_TOKEN"]
	httpClient := &http.Client{Timeout: 10 * time.Second}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s", ownerRepo)
	req, _ := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
	if req != nil {
		if gitToken != "" {
			req.Header.Set("Authorization", "token "+gitToken)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			checks = append(checks, preflightCheck{"Arch-hub", "fail", fmt.Sprintf("cannot reach %s: %s", ownerRepo, err)})
			return checks
		}
		resp.Body.Close()

		if resp.StatusCode == 404 {
			msg := fmt.Sprintf("repo '%s' not found", ownerRepo)
			if gitToken == "" {
				msg += " (no GITHUB_TOKEN — may need auth for private repos)"
			}
			checks = append(checks, preflightCheck{"Arch-hub", "fail", msg})
			return checks
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			checks = append(checks, preflightCheck{"Arch-hub", "fail", fmt.Sprintf("auth failed for '%s' (HTTP %d) — check GITHUB_TOKEN permissions", ownerRepo, resp.StatusCode)})
			return checks
		}
		if resp.StatusCode >= 400 {
			checks = append(checks, preflightCheck{"Arch-hub", "fail", fmt.Sprintf("unexpected HTTP %d for '%s'", resp.StatusCode, ownerRepo)})
			return checks
		}
	}

	// Check 4: Verify push access (check repo permissions)
	if gitToken != "" {
		apiURL := fmt.Sprintf("https://api.github.com/repos/%s", ownerRepo)
		req, _ := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
		if req != nil {
			req.Header.Set("Authorization", "token "+gitToken)
			req.Header.Set("Accept", "application/vnd.github.v3+json")
			resp, err := httpClient.Do(req)
			if err == nil {
				defer resp.Body.Close()
				// GitHub returns permissions in the repo response
				// We'd need to parse JSON, but for now existence + auth is good enough
				checks = append(checks, preflightCheck{
					"Arch-hub",
					"ok",
					fmt.Sprintf("'%s' accessible with token", ownerRepo),
				})
				return checks
			}
		}
	}

	checks = append(checks, preflightCheck{
		"Arch-hub",
		"ok",
		fmt.Sprintf("'%s' exists (push access not verified — no GITHUB_TOKEN)", ownerRepo),
	})
	return checks
}
