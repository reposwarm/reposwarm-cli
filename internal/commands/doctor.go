package commands

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/config"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

// checkResult holds a single health check.
type checkResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "ok", "warn", "fail"
	Message string `json:"message"`
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose RepoSwarm installation health",
		Long: `Runs a series of checks to verify your RepoSwarm setup is working:
  - CLI configuration (API URL, token)
  - API server connectivity and health
  - Temporal server connectivity
  - DynamoDB connectivity
  - Worker status
  - Local dependencies (Docker, Node, Python, Git)
  - Network connectivity`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var checks []checkResult

			if !flagJSON {
				output.F.Section("RepoSwarm Doctor")
			}

			// 1. Config file
			checks = append(checks, checkConfig()...)

			// 2. API connectivity
			checks = append(checks, checkAPI()...)

			// 3. Local tools
			checks = append(checks, checkLocalTools()...)

			// 4. Network
			checks = append(checks, checkNetwork()...)

			// 5. Worker environment (local mode)
			checks = append(checks, checkWorkerEnv()...)

			// 6. Worker logs (local mode)
			checks = append(checks, checkWorkerLogs()...)

			// 7. Per-worker health (multi-worker)
			checks = append(checks, checkPerWorkerHealth()...)

			// 8. Stalled workflows
			checks = append(checks, checkStalledWorkflows()...)

			// 9. Provider credentials and inference check
			checks = append(checks, checkProviderCredentials()...)

			if flagJSON {
				summary := map[string]any{
					"checks": checks,
					"ok":     countStatus(checks, "ok"),
					"warn":   countStatus(checks, "warn"),
					"fail":   countStatus(checks, "fail"),
				}
				return output.JSON(summary)
			}

			// Summary
			ok := countStatus(checks, "ok")
			warn := countStatus(checks, "warn")
			fail := countStatus(checks, "fail")
			output.F.CheckSummary(ok, warn, fail)
			return nil
		},
	}
}

func printCheck(c checkResult) {
	if flagJSON {
		return
	}
	output.F.CheckResult(c.Name, c.Status, c.Message)
}

func checkConfig() []checkResult {
	var results []checkResult

	cfg, err := config.Load()
	if err != nil {
		c := checkResult{"Config file", "fail", fmt.Sprintf("error loading: %s", err)}
		printCheck(c)
		return append(results, c)
	}

	// Config path
	path, _ := config.ConfigPath()
	if _, err := os.Stat(path); err != nil {
		c := checkResult{"Config file", "warn", "no config file — using defaults. Run 'reposwarm config init'"}
		printCheck(c)
		results = append(results, c)
	} else {
		c := checkResult{"Config file", "ok", path}
		printCheck(c)
		results = append(results, c)
	}

	// API URL
	if cfg.APIUrl == "" {
		c := checkResult{"API URL", "fail", "not configured"}
		printCheck(c)
		results = append(results, c)
	} else {
		c := checkResult{"API URL", "ok", cfg.APIUrl}
		printCheck(c)
		results = append(results, c)
	}

	// API Token
	if cfg.APIToken == "" {
		c := checkResult{"API token", "fail", "not configured — run 'reposwarm config init'"}
		printCheck(c)
		results = append(results, c)
	} else {
		c := checkResult{"API token", "ok", config.MaskedToken(cfg.APIToken)}
		printCheck(c)
		results = append(results, c)
	}

	return results
}

func checkAPI() []checkResult {
	var results []checkResult

	client, err := getClient()
	if err != nil {
		c := checkResult{"API connection", "fail", fmt.Sprintf("cannot create client: %s", err)}
		printCheck(c)
		return append(results, c)
	}

	start := time.Now()
	health, err := client.Health(context.Background())
	latency := time.Since(start)

	if err != nil {
		c := checkResult{"API connection", "fail", fmt.Sprintf("unreachable: %s", err)}
		printCheck(c)
		results = append(results, c)
		return results
	}

	c := checkResult{"API connection", "ok", fmt.Sprintf("%s (%dms)", health.Status, latency.Milliseconds())}
	printCheck(c)
	results = append(results, c)

	// Temporal
	if health.Temporal.Connected {
		c = checkResult{"Temporal", "ok", fmt.Sprintf("connected (ns: %s, queue: %s)", health.Temporal.Namespace, health.Temporal.TaskQueue)}
	} else {
		c = checkResult{"Temporal", "fail", "not connected"}
	}
	printCheck(c)
	results = append(results, c)

	// DynamoDB
	if health.DynamoDB.Connected {
		c = checkResult{"DynamoDB", "ok", "connected"}
	} else {
		c = checkResult{"DynamoDB", "fail", "not connected"}
	}
	printCheck(c)
	results = append(results, c)

	// Worker
	if health.Worker.Connected {
		c = checkResult{"Worker", "ok", fmt.Sprintf("connected (%d active)", health.Worker.Count)}
	} else {
		c = checkResult{"Worker", "warn", "no worker connected — investigations will queue but not run"}
	}
	printCheck(c)
	results = append(results, c)

	return results
}

func checkLocalTools() []checkResult {
	var results []checkResult

	tools := []struct {
		name    string
		cmd     string
		args    []string
		level   string // "fail" or "warn" if missing
	}{
		{"Git", "git", []string{"--version"}, "warn"},
		{"Docker", "docker", []string{"--version"}, "warn"},
		{"Node.js", "node", []string{"--version"}, "warn"},
		{"Python", "python3", []string{"--version"}, "warn"},
		{"AWS CLI", "aws", []string{"--version"}, "warn"},
	}

	for _, t := range tools {
		out, err := exec.Command(t.cmd, t.args...).Output()
		if err != nil {
			c := checkResult{t.name, t.level, "not found"}
			printCheck(c)
			results = append(results, c)
		} else {
			ver := strings.TrimSpace(string(out))
			if len(ver) > 60 {
				ver = ver[:60] + "..."
			}
			c := checkResult{t.name, "ok", ver}
			printCheck(c)
			results = append(results, c)
		}
	}

	return results
}

func checkNetwork() []checkResult {
	var results []checkResult

	// DNS resolution
	_, err := net.LookupHost("github.com")
	if err != nil {
		c := checkResult{"DNS", "fail", "cannot resolve github.com"}
		printCheck(c)
		results = append(results, c)
	} else {
		c := checkResult{"DNS", "ok", "resolving"}
		printCheck(c)
		results = append(results, c)
	}

	// GitHub connectivity
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com")
	if err != nil {
		c := checkResult{"GitHub API", "warn", fmt.Sprintf("unreachable: %s", err)}
		printCheck(c)
		results = append(results, c)
	} else {
		resp.Body.Close()
		c := checkResult{"GitHub API", "ok", fmt.Sprintf("HTTP %d", resp.StatusCode)}
		printCheck(c)
		results = append(results, c)
	}

	return results
}

func checkPerWorkerHealth() []checkResult {
	var results []checkResult

	client, err := getClient()
	if err != nil {
		return results
	}

	workers := gatherWorkerInfo(client)
	if len(workers) <= 1 {
		return results // Single worker already covered by existing checks
	}

	if !flagJSON {
		output.F.Println()
		output.F.Section(fmt.Sprintf("Workers (%d)", len(workers)))
	}

	healthy := 0
	for _, w := range workers {
		if w.Status == "healthy" {
			healthy++
		}
		status := "ok"
		msg := w.Status
		if w.Status != "healthy" {
			status = "fail"
			if len(w.EnvErrors) > 0 {
				msg += fmt.Sprintf(" (%s)", strings.Join(w.EnvErrors, ", "))
			}
		}

		c := checkResult{
			Name:    fmt.Sprintf("[%s] Status", w.Name),
			Status:  status,
			Message: msg,
		}
		printCheck(c)
		results = append(results, c)
	}

	return results
}

func checkStalledWorkflows() []checkResult {
	var results []checkResult

	client, err := getClient()
	if err != nil {
		return results
	}

	var wfResult api.WorkflowsResponse
	if err := client.Get(ctx(), "/workflows?pageSize=20", &wfResult); err != nil {
		return results
	}

	for _, w := range wfResult.Executions {
		if w.Status != "RUNNING" && w.Status != "Running" {
			continue
		}

		startTime, err := time.Parse(time.RFC3339, w.StartTime)
		if err != nil {
			continue
		}
		runDuration := time.Since(startTime)

		if runDuration < 30*time.Minute {
			continue
		}

		// Check progress via wiki
		repo := repoName(w.WorkflowID)
		var wikiIndex api.WikiIndex
		sections := 0
		if err := client.Get(ctx(), "/wiki/"+repo, &wikiIndex); err == nil {
			sections = len(wikiIndex.Sections)
		}

		if sections == 0 {
			if !flagJSON && len(results) == 0 {
				output.F.Println()
				output.F.Section("Stalled Workflows")
			}

			c := checkResult{
				Name:    repo,
				Status:  "warn",
				Message: fmt.Sprintf("0/17 steps, running %s, no progress", formatRelativeTime(runDuration)),
			}
			printCheck(c)
			results = append(results, c)
		}
	}

	return results
}

func checkWorkerEnv() []checkResult {
	var results []checkResult

	client, err := getClient()
	if err != nil {
		return results
	}

	if !flagJSON {
		output.F.Println()
		output.F.Section("Worker Environment")
	}

	// Fetch worker env from API
	var envResp struct {
		Entries []struct {
			Key    string `json:"key"`
			Set    bool   `json:"set"`
		} `json:"entries"`
	}
	if err := client.Get(ctx(), "/workers/worker-1/env", &envResp); err != nil {
		// API doesn't support /workers endpoint yet — skip silently
		return results
	}

	required := map[string]string{
		"ANTHROPIC_API_KEY": "Anthropic API key (for LLM calls)",
		"GITHUB_TOKEN":      "GitHub token (for repo access)",
	}

	for _, entry := range envResp.Entries {
		desc, isRequired := required[entry.Key]
		if !isRequired {
			continue
		}
		if entry.Set {
			c := checkResult{entry.Key, "ok", "set"}
			printCheck(c)
			results = append(results, c)
		} else {
			c := checkResult{entry.Key, "fail", fmt.Sprintf("NOT SET \u2014 %s", desc)}
			printCheck(c)
			results = append(results, c)
		}
	}

	return results
}


func checkWorkerLogs() []checkResult {
	var results []checkResult

	client, err := getClient()
	if err != nil {
		return results
	}

	if !flagJSON {
		output.F.Println()
		output.F.Section("Worker Logs")
	}

	var logResp struct {
		Lines []string `json:"lines"`
	}
	if err := client.Get(ctx(), "/services/worker/logs?lines=20", &logResp); err != nil {
		return results
	}

	if len(logResp.Lines) == 0 {
		return results
	}

	// Scan for errors
	errorPatterns := []string{"error", "Error", "ERROR", "failed", "Failed", "FAILED", "Traceback", "Exception", "ValidationError"}
	var errorLines []string
	for _, line := range logResp.Lines {
		for _, pattern := range errorPatterns {
			if strings.Contains(line, pattern) {
				errorLines = append(errorLines, line)
				break
			}
		}
	}

	if len(errorLines) > 0 {
		shown := errorLines
		if len(shown) > 5 {
			shown = shown[len(shown)-5:]
		}
		msg := fmt.Sprintf("%d error(s) in last 20 lines", len(errorLines))
		c := checkResult{"Worker log", "warn", msg}
		printCheck(c)
		results = append(results, c)

		if !flagJSON {
			for _, line := range shown {
				trimmed := line
				if len(trimmed) > 120 {
					trimmed = trimmed[:117] + "..."
				}
				output.F.Printf("    %s\n", output.Yellow(trimmed))
			}
		}
	} else {
		c := checkResult{"Worker log", "ok", "no recent errors"}
		printCheck(c)
		results = append(results, c)
	}

	return results
}


func countStatus(checks []checkResult, status string) int {
	n := 0
	for _, c := range checks {
		if c.Status == status {
			n++
		}
	}
	return n
}

func checkProviderCredentials() []checkResult {
	var results []checkResult

	cfg, err := config.Load()
	if err != nil {
		return results
	}

	client, err := getClient()
	if err != nil {
		return results
	}

	if !flagJSON {
		output.F.Println()
		output.F.Section("Provider Credentials")
	}

	// Get current provider config
	provider := cfg.EffectiveProvider()
	pc := cfg.ProviderConfig

	// Fetch worker env for validation
	var envResp struct {
		Entries []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
			Set   bool   `json:"set"`
		} `json:"entries"`
	}

	currentEnv := make(map[string]string)
	if err := client.Get(ctx(), "/workers/worker-1/env?reveal=true", &envResp); err == nil {
		for _, e := range envResp.Entries {
			if e.Set {
				currentEnv[e.Key] = e.Value
			}
		}
	}

	// Validate environment
	validation := config.ValidateWorkerEnv(&pc, currentEnv)

	// Provider-specific credential checks
	switch provider {
	case config.ProviderAnthropic:
		if apiKey, ok := currentEnv["ANTHROPIC_API_KEY"]; ok && apiKey != "" {
			c := checkResult{"Anthropic API key", "ok", "set"}
			printCheck(c)
			results = append(results, c)
		} else {
			c := checkResult{"Anthropic API key", "fail", "NOT SET"}
			printCheck(c)
			results = append(results, c)
		}

	case config.ProviderBedrock:
		// Check AWS credentials based on auth method
		authMethod := pc.BedrockAuth
		if authMethod == "" {
			authMethod = config.BedrockAuthIAMRole
		}

		switch authMethod {
		case config.BedrockAuthLongTermKeys:
			hasKey := currentEnv["AWS_ACCESS_KEY_ID"] != ""
			hasSecret := currentEnv["AWS_SECRET_ACCESS_KEY"] != ""
			if hasKey && hasSecret {
				c := checkResult{"AWS credentials", "ok", "long-term keys set"}
				printCheck(c)
				results = append(results, c)
			} else {
				c := checkResult{"AWS credentials", "fail", "long-term keys NOT SET"}
				printCheck(c)
				results = append(results, c)
			}

		case config.BedrockAuthProfile, config.BedrockAuthSSO:
			if profile, ok := currentEnv["AWS_PROFILE"]; ok && profile != "" {
				c := checkResult{"AWS profile", "ok", profile}
				printCheck(c)
				results = append(results, c)
			} else {
				c := checkResult{"AWS profile", "fail", "NOT SET"}
				printCheck(c)
				results = append(results, c)
			}

		case config.BedrockAuthIAMRole:
			c := checkResult{"AWS credentials", "ok", "using IAM role"}
			printCheck(c)
			results = append(results, c)
		}

		// Check region
		if region, ok := currentEnv["AWS_REGION"]; ok && region != "" {
			c := checkResult{"AWS region", "ok", region}
			printCheck(c)
			results = append(results, c)
		} else {
			c := checkResult{"AWS region", "warn", "not set (will use default)"}
			printCheck(c)
			results = append(results, c)
		}

	case config.ProviderLiteLLM:
		if proxyURL, ok := currentEnv["ANTHROPIC_BASE_URL"]; ok && proxyURL != "" {
			c := checkResult{"LiteLLM proxy URL", "ok", proxyURL}
			printCheck(c)
			results = append(results, c)
		} else {
			c := checkResult{"LiteLLM proxy URL", "fail", "NOT SET"}
			printCheck(c)
			results = append(results, c)
		}
	}

	// Run inference health check
	if !flagJSON {
		fmt.Print("  Inference check: ")
	}

	var inferenceResp struct {
		Success     bool   `json:"success"`
		Provider    string `json:"provider"`
		Model       string `json:"model"`
		AuthMethod  string `json:"authMethod"`
		LatencyMs   int    `json:"latencyMs"`
		Response    string `json:"response"`
		Error       string `json:"error"`
		Hint        string `json:"hint"`
	}

	if err := client.Post(ctx(), "/workers/worker-1/inference-check", nil, &inferenceResp); err != nil {
		// API endpoint might not exist yet
		c := checkResult{"Inference check", "warn", "endpoint not available"}
		if !flagJSON {
			fmt.Println("endpoint not available")
		}
		results = append(results, c)
	} else {
		if inferenceResp.Success {
			c := checkResult{"Inference check", "ok", fmt.Sprintf("working (%dms)", inferenceResp.LatencyMs)}
			if !flagJSON {
				output.Successf("✓ working (%dms)", inferenceResp.LatencyMs)
			}
			results = append(results, c)
		} else {
			errorMsg := inferenceResp.Error
			if inferenceResp.Hint != "" {
				errorMsg += " — " + inferenceResp.Hint
			}
			c := checkResult{"Inference check", "fail", errorMsg}
			if !flagJSON {
				output.F.Error(fmt.Sprintf("✗ %s", errorMsg))
			}
			results = append(results, c)
		}
	}

	// Overall validation status
	if !validation.Valid && len(validation.Missing) > 0 {
		for _, missing := range validation.Missing {
			c := checkResult{
				Name:    fmt.Sprintf("Required: %s", missing.Key),
				Status:  "fail",
				Message: fmt.Sprintf("NOT SET — %s", missing.Desc),
			}
			// Don't double-print if we already checked it above
			if !containsCheckForKey(results, missing.Key) {
				printCheck(c)
				results = append(results, c)
			}
		}
	}

	return results
}

func containsCheckForKey(checks []checkResult, key string) bool {
	for _, c := range checks {
		if strings.Contains(c.Name, key) {
			return true
		}
	}
	return false
}
