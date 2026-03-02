package commands

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"bufio"
	"path/filepath"
	"strings"
	"time"

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

func checkWorkerEnv() []checkResult {
	var results []checkResult

	cfg, err := config.Load()
	if err != nil {
		return results
	}

	installDir := cfg.EffectiveInstallDir()
	envFile := filepath.Join(installDir, "worker", ".env")

	// Check if local install exists
	if _, err := os.Stat(installDir); os.IsNotExist(err) {
		// Not a local install, skip
		return results
	}

	if !flagJSON {
		output.F.Println()
		output.F.Section("Worker Environment")
	}

	// Read .env file if it exists
	envVars := make(map[string]bool)
	if data, err := os.ReadFile(envFile); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
				envVars[parts[0]] = true
			}
		}
	}

	// Also check actual environment
	checkEnv := func(name string) bool {
		if envVars[name] {
			return true
		}
		return os.Getenv(name) != ""
	}

	// Required vars
	required := []struct {
		name string
		alts []string // alternative names
		desc string
	}{
		{"ANTHROPIC_API_KEY", nil, "Anthropic API key (for LLM calls)"},
		{"GITHUB_TOKEN", []string{"GITHUB_PAT"}, "GitHub token (for repo access)"},
	}

	for _, req := range required {
		found := checkEnv(req.name)
		if !found {
			for _, alt := range req.alts {
				if checkEnv(alt) {
					found = true
					break
				}
			}
		}
		if found {
			c := checkResult{req.name, "ok", "set"}
			printCheck(c)
			results = append(results, c)
		} else {
			allNames := req.name
			if len(req.alts) > 0 {
				allNames += "/" + strings.Join(req.alts, "/")
			}
			c := checkResult{allNames, "fail", fmt.Sprintf("NOT SET — %s", req.desc)}
			printCheck(c)
			results = append(results, c)
		}
	}

	// AWS credentials (env vars or instance profile)
	hasAWSEnv := checkEnv("AWS_ACCESS_KEY_ID") && checkEnv("AWS_SECRET_ACCESS_KEY")
	if hasAWSEnv {
		c := checkResult{"AWS credentials", "ok", "set (env vars)"}
		printCheck(c)
		results = append(results, c)
	} else {
		// Check for instance metadata (EC2 role)
		client := &http.Client{Timeout: 1 * time.Second}
		req, _ := http.NewRequest("PUT", "http://169.254.169.254/latest/api/token", nil)
		if req != nil {
			req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				c := checkResult{"AWS credentials", "ok", "set (instance profile)"}
				printCheck(c)
				results = append(results, c)
			} else {
				c := checkResult{"AWS credentials", "warn", "not found (no env vars or instance profile)"}
				printCheck(c)
				results = append(results, c)
			}
		}
	}

	return results
}

func checkWorkerLogs() []checkResult {
	var results []checkResult

	cfg, err := config.Load()
	if err != nil {
		return results
	}

	installDir := cfg.EffectiveInstallDir()
	logFile := filepath.Join(installDir, "logs", "worker.log")

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		return results // No log file, skip silently
	}

	if !flagJSON {
		output.F.Println()
		output.F.Section("Worker Logs")
	}

	// Read last 20 lines and check for errors
	f, err := os.Open(logFile)
	if err != nil {
		c := checkResult{"Worker log", "warn", fmt.Sprintf("cannot read: %s", err)}
		printCheck(c)
		return append(results, c)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Keep last 20 lines
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}

	// Scan for error patterns
	errorPatterns := []string{"error", "Error", "ERROR", "failed", "Failed", "FAILED", "Traceback", "traceback", "Exception", "ValidationError"}
	var errorLines []string
	for _, line := range lines {
		for _, pattern := range errorPatterns {
			if strings.Contains(line, pattern) {
				errorLines = append(errorLines, line)
				break
			}
		}
	}

	if len(errorLines) > 0 {
		// Trim to last 5 error lines
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
