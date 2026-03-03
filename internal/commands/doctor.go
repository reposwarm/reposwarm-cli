package commands

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
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

func newDoctorCmd(currentVersion string) *cobra.Command {
	var fixMode bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose RepoSwarm installation health",
		Long: `Runs a series of checks to verify your RepoSwarm setup is working:
  - CLI configuration (API URL, token)
  - API server connectivity and health
  - Version compatibility (CLI ↔ API ↔ UI)
  - CLI update availability
  - Temporal server connectivity
  - DynamoDB connectivity
  - Worker status
  - Local dependencies (Docker, Node, Python, Git)
  - Network connectivity
  - Provider credentials

Use --fix to attempt automatic fixes for failed checks.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var checks []checkResult

			if !flagJSON {
				output.F.Section("RepoSwarm Doctor")
			}

			// 1. Config file
			checks = append(checks, checkConfig()...)

			// 2. API connectivity
			checks = append(checks, checkAPI()...)

			// 2b. Version compatibility
			checks = append(checks, checkVersionCompat()...)

			// 2c. CLI update check
			checks = append(checks, checkCLIUpdate(currentVersion)...)

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

			// Recommended actions section
			if fail > 0 || warn > 0 {
				actions := buildRecommendedActions(checks)
				if len(actions) > 0 {
					fmt.Println()
					output.F.Section("Recommended Actions")
					fmt.Println()
					for i, a := range actions {
						fmt.Printf("  %d. %s\n", i+1, a.desc)
						fmt.Printf("     %s\n\n", output.Cyan(a.cmd))
					}

					if fixMode {
						fmt.Println()
						output.F.Section("Auto-Fix")
						runAutoFixes(checks)
					} else {
						fmt.Printf("  Run all fixes: %s\n\n", output.Cyan("reposwarm doctor --fix"))
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&fixMode, "fix", false, "Attempt automatic fixes for failed checks")
	return cmd
}

func printCheck(c checkResult) {
	if flagJSON {
		return
	}
	output.F.CheckResult(c.Name, c.Status, c.Message)
}

// fixAction maps check names to auto-fix actions.
type fixAction struct {
	Name    string
	Fix     func() error
	Desc    string
}

func runAutoFixes(checks []checkResult) {
	fixes := map[string]fixAction{
		"API Server version": {
			Name: "Upgrade API server",
			Desc: "reposwarm upgrade api",
			Fix: func() error {
				return upgradeService("api", false)
			},
		},
		"UI version": {
			Name: "Upgrade UI",
			Desc: "reposwarm upgrade ui",
			Fix: func() error {
				return upgradeService("ui", false)
			},
		},
	}

	fixedCount := 0
	for _, c := range checks {
		if c.Status != "fail" {
			continue
		}
		if fa, ok := fixes[c.Name]; ok {
			fmt.Printf("  Fixing: %s (%s)... ", c.Name, fa.Desc)
			if err := fa.Fix(); err != nil {
				output.F.Error(fmt.Sprintf("failed: %v", err))
			} else {
				output.Successf("done")
				fixedCount++
			}
		} else {
			// No auto-fix, show manual instructions
			output.F.Info(fmt.Sprintf("  %s — no auto-fix available. Fix manually:", c.Name))
			switch {
			case strings.Contains(c.Name, "Worker"):
				output.F.Info("    reposwarm restart worker")
			case strings.Contains(c.Name, "Provider") || strings.Contains(c.Name, "Inference"):
				output.F.Info("    reposwarm config provider setup")
			case strings.Contains(c.Name, "Docker"):
				output.F.Info("    Install Docker: https://docs.docker.com/get-docker/")
			case strings.Contains(c.Name, "Temporal"):
				output.F.Info("    reposwarm restart temporal")
			default:
				output.F.Info("    Check the error message above for guidance")
			}
		}
	}

	if fixedCount > 0 {
		fmt.Println()
		output.Successf("%d fix(es) applied — re-run 'reposwarm doctor' to verify", fixedCount)
	}
}

func checkVersionCompat() []checkResult {
	var results []checkResult

	client, err := getClient()
	if err != nil {
		return results
	}

	if !flagJSON {
		output.F.Println()
		output.F.Section("Version Compatibility")
	}

	// Get API version from health endpoint
	health, err := client.Health(context.Background())
	apiVersion := ""
	if err == nil && health.Version != "" {
		apiVersion = health.Version
	}

	// Get UI version (try /version or /)
	uiVersion := ""
	// UI version check is best-effort — skip for now

	versionChecks := config.CheckVersions(apiVersion, uiVersion)
	for _, vc := range versionChecks {
		if vc.Compatible {
			c := checkResult{
				Name:    fmt.Sprintf("%s version", vc.Component),
				Status:  "ok",
				Message: fmt.Sprintf("v%s (min: v%s)", vc.Actual, vc.Minimum),
			}
			printCheck(c)
			results = append(results, c)
		} else {
			c := checkResult{
				Name:    fmt.Sprintf("%s version", vc.Component),
				Status:  "fail",
				Message: fmt.Sprintf("v%s — needs v%s+ (run: reposwarm upgrade %s)", vc.Actual, vc.Minimum, strings.ToLower(vc.Component)),
			}
			printCheck(c)
			results = append(results, c)
		}
	}

	if apiVersion == "" {
		c := checkResult{
			Name:    "API version",
			Status:  "warn",
			Message: "could not determine API version",
		}
		printCheck(c)
		results = append(results, c)
	}

	return results
}

func checkCLIUpdate(currentVersion string) []checkResult {
	var results []checkResult

	if !flagJSON {
		output.F.Println()
		output.F.Section("CLI Updates")
	}

	latestVer, _, err := getLatestRelease()
	if err != nil {
		c := checkResult{Name: "CLI update", Status: "warn", Message: "could not check for updates"}
		printCheck(c)
		return append(results, c)
	}

	if latestVer == currentVersion {
		c := checkResult{Name: "CLI version", Status: "ok", Message: fmt.Sprintf("v%s (latest)", currentVersion)}
		printCheck(c)
		return append(results, c)
	}

	// New version available — show changelog
	c := checkResult{
		Name:    "CLI version",
		Status:  "warn",
		Message: fmt.Sprintf("v%s → v%s available (run: reposwarm upgrade)", currentVersion, latestVer),
	}
	printCheck(c)
	results = append(results, c)

	// Fetch and display changelog
	changes, chErr := getChangelog(currentVersion, latestVer)
	if chErr == nil && len(changes) > 0 {
		if !flagJSON {
			fmt.Println()
			output.F.Info("What's new:")
			for _, line := range changes {
				fmt.Printf("    %s\n", line)
			}
		}
	}

	return results
}

// recommendedAction pairs a human description with a copyable command.
type recommendedAction struct {
	desc string
	cmd  string
}

func buildRecommendedActions(checks []checkResult) []recommendedAction {
	var actions []recommendedAction
	seen := map[string]bool{}

	for _, c := range checks {
		if c.Status != "fail" && c.Status != "warn" {
			continue
		}

		var cmd, desc string

		switch {
		case strings.Contains(c.Name, "API Server version"):
			cmd = "reposwarm upgrade api"
			desc = "Upgrade API server to latest version"
		case strings.Contains(c.Name, "UI version"):
			cmd = "reposwarm upgrade ui"
			desc = "Upgrade UI to latest version"
		case strings.Contains(c.Name, "CLI version") && strings.Contains(c.Message, "available"):
			cmd = "reposwarm upgrade"
			desc = "Upgrade CLI to latest version"
		case strings.Contains(c.Name, "Provider") || strings.Contains(c.Name, "Inference"):
			cmd = "reposwarm config provider setup"
			desc = "Configure LLM provider settings"
		case strings.Contains(c.Name, "Worker") && strings.Contains(c.Message, "not running"):
			cmd = "reposwarm restart worker"
			desc = "Restart the worker process"
		case strings.Contains(c.Name, "Temporal"):
			cmd = "reposwarm restart temporal"
			desc = "Restart Temporal server"
		case strings.Contains(c.Name, "API") && strings.Contains(c.Message, "not reachable"):
			cmd = "reposwarm restart api"
			desc = "Restart the API server"
		case strings.Contains(c.Message, "NOT SET") && !strings.Contains(c.Name, "Required"):
			// Missing env var — offer direct set command
			cmd = fmt.Sprintf("reposwarm config worker-env set %s <value>", c.Name)
			desc = fmt.Sprintf("Set missing env var: %s", c.Name)
		case strings.Contains(c.Name, "Required:") && strings.Contains(c.Message, "NOT SET"):
			cmd = "reposwarm config provider setup"
			desc = "Configure provider (sets required env vars)"
		default:
			continue
		}

		if cmd != "" && !seen[cmd] {
			seen[cmd] = true
			actions = append(actions, recommendedAction{desc: desc, cmd: cmd})
		}
	}

	return actions
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

	required := map[string]string{}

	// GITHUB_TOKEN is optional — only needed for private repos
	optional := map[string]string{
		"GITHUB_TOKEN": "GitHub token (only needed for private repos)",
	}

	// Add provider-specific requirements (don't hardcode ANTHROPIC_API_KEY for all providers)
	cfg, cfgErr := config.Load()
	if cfgErr == nil {
		for _, req := range config.RequiredEnvVars(&cfg.ProviderConfig) {
			if req.Required {
				required[req.Key] = req.Desc
			}
		}
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
			c := checkResult{entry.Key, "fail", fmt.Sprintf("NOT SET — %s", desc)}
			printCheck(c)
			results = append(results, c)
			if !flagJSON {
				fmt.Printf("     Set it: %s\n", output.Cyan(fmt.Sprintf("reposwarm config worker-env set %s <value>", entry.Key)))
			}
		}
	}

	// Check optional env vars (warn, not fail)
	for _, entry := range envResp.Entries {
		desc, isOptional := optional[entry.Key]
		if !isOptional {
			continue
		}
		if entry.Set {
			c := checkResult{entry.Key, "ok", "set"}
			printCheck(c)
			results = append(results, c)
		} else {
			c := checkResult{entry.Key, "warn", fmt.Sprintf("not set — %s", desc)}
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

	// Scan for errors — skip summary lines like "Found X errors and Y warnings"
	errorPatterns := []string{"error", "Error", "ERROR", "failed", "Failed", "FAILED", "Traceback", "Exception", "ValidationError"}
	summaryPattern := regexp.MustCompile(`(?i)found \d+ errors? and \d+ warnings?`)
	envValidationFailed := regexp.MustCompile(`(?i)environment validation failed`)
	// Match individual validation issue lines like "❌ ERROR: GITHUB_TOKEN not set" or "⚠️ WARNING: ..."
	validationIssue := regexp.MustCompile(`(?:ERROR|WARNING):\s+(.+)`)

	var errorLines []string
	var validationIssues []string
	var workerErrors, workerWarnings int

	for _, line := range logResp.Lines {
		// Extract the worker's own validation summary if present
		if m := regexp.MustCompile(`Found (\d+) errors? and (\d+) warnings?`).FindStringSubmatch(line); len(m) == 3 {
			fmt.Sscanf(m[1], "%d", &workerErrors)
			fmt.Sscanf(m[2], "%d", &workerWarnings)
			continue
		}

		// Skip "ENVIRONMENT VALIDATION FAILED" header
		if envValidationFailed.MatchString(line) {
			continue
		}

		// Skip summary lines
		if summaryPattern.MatchString(line) {
			continue
		}

		// Capture individual validation errors/warnings
		if m := validationIssue.FindStringSubmatch(line); len(m) == 2 {
			// Extract just the issue text, strip log prefix
			issue := strings.TrimSpace(m[1])
			if strings.Contains(line, "ERROR") {
				validationIssues = append(validationIssues, fmt.Sprintf("✗ %s", issue))
			} else {
				validationIssues = append(validationIssues, fmt.Sprintf("⚠ %s", issue))
			}
			continue // Don't also count as a generic error line
		}

		for _, pattern := range errorPatterns {
			if strings.Contains(line, pattern) {
				errorLines = append(errorLines, line)
				break
			}
		}
	}

	if workerErrors > 0 || workerWarnings > 0 || len(validationIssues) > 0 {
		msg := fmt.Sprintf("env validation: %s, %s",
			pluralizeCount(workerErrors, "error"), pluralizeCount(workerWarnings, "warning"))
		c := checkResult{"Worker validation", "warn", msg}
		printCheck(c)
		results = append(results, c)

		// Show the actual issues
		if !flagJSON && len(validationIssues) > 0 {
			for _, issue := range validationIssues {
				if strings.HasPrefix(issue, "✗") {
					fmt.Printf("    %s\n", output.Red(issue))
				} else {
					fmt.Printf("    %s\n", output.Yellow(issue))
				}
			}
		}
	}

	if len(errorLines) > 0 {
		shown := errorLines
		if len(shown) > 5 {
			shown = shown[len(shown)-5:]
		}
		msg := fmt.Sprintf("%s in last 20 log lines", pluralizeCount(len(errorLines), "error"))
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
	} else if workerErrors == 0 && workerWarnings == 0 {
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

func pluralizeCount(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
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
				output.Successf("working (%dms)", inferenceResp.LatencyMs)
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
				if !flagJSON {
					if missing.Key == "ANTHROPIC_API_KEY" || missing.Key == "AWS_BEARER_TOKEN_BEDROCK" {
						// Sensitive — point to provider setup instead of direct set
						fmt.Printf("     Configure: %s\n", output.Cyan("reposwarm config provider setup"))
					} else {
						fmt.Printf("     Set it: %s\n", output.Cyan(fmt.Sprintf("reposwarm config worker-env set %s <value>", missing.Key)))
					}
				}
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
