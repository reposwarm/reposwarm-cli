package commands

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/config"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newWorkersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workers",
		Aliases: []string{"worker"},
		Short:   "Manage and inspect workers",
	}
	cmd.AddCommand(newWorkersListCmd())
	cmd.AddCommand(newWorkersShowCmd())
	return cmd
}

func newWorkersListCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all workers with health and activity status",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			workers := gatherWorkerInfo(client)

			if flagJSON {
				healthy := 0
				for _, w := range workers {
					if w.Status == "healthy" {
						healthy++
					}
				}
				return output.JSON(map[string]any{
					"workers": workers,
					"total":   len(workers),
					"healthy": healthy,
				})
			}

			healthy := 0
			for _, w := range workers {
				if w.Status == "healthy" {
					healthy++
				}
			}

			F := output.F
			F.Section(fmt.Sprintf("Workers (%d configured, %d healthy)", len(workers), healthy))

			if len(workers) == 0 {
				F.Warning("No workers detected")
				F.Info("Workers register with Temporal when they start.")
				F.Info("Check: reposwarm logs worker")
				return nil
			}

			headers := []string{"Name", "Status", "Queue", "Current Task", "Last Activity", "Env"}
			if verbose {
				headers = append(headers, "PID", "Host")
			}
			var rows [][]string
			for _, w := range workers {
				statusStr := w.Status
				switch w.Status {
				case "healthy":
					statusStr = output.Green("✅ healthy")
				case "degraded":
					statusStr = output.Yellow("⚠ degraded")
				case "failed":
					statusStr = output.Red("❌ failed")
				case "stopped":
					statusStr = output.Dim("⏹ stopped")
				}

				envStr := output.Green("OK")
				if len(w.EnvErrors) > 0 {
					envStr = output.Red(fmt.Sprintf("%d env errors", len(w.EnvErrors)))
				}

				currentTask := w.CurrentTask
				if currentTask == "" {
					currentTask = output.Dim("idle")
				}

				lastAct := w.LastActivity
				if lastAct == "" {
					lastAct = output.Dim("never")
				}

				row := []string{w.Name, statusStr, w.TaskQueue, currentTask, lastAct, envStr}
				if verbose {
					pid := "—"
					if w.PID > 0 {
						pid = fmt.Sprint(w.PID)
					}
					host := w.Host
					if host == "" {
						host = "—"
					}
					row = append(row, pid, host)
				}
				rows = append(rows, row)
			}

			output.Table(headers, rows)
			F.Println()
			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Include PID, uptime, host")
	return cmd
}

func newWorkersShowCmd() *cobra.Command {
	var logLines int
	var noLogs bool

	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Deep-dive on a single worker: env, logs, current task",
		Args:  friendlyExactArgs(1, "reposwarm workers show <name>\n\nExample:\n  reposwarm workers show worker-1"),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetName := args[0]

			client, err := getClient()
			if err != nil {
				return err
			}

			workers := gatherWorkerInfo(client)

			// Find target worker
			var worker *api.WorkerInfo
			for _, w := range workers {
				if w.Name == targetName || w.Identity == targetName {
					wCopy := w
					worker = &wCopy
					break
				}
			}

			if worker == nil {
				// If only one worker and name doesn't match, show it anyway
				if len(workers) == 1 && (targetName == "worker" || targetName == "worker-1" || targetName == "1") {
					worker = &workers[0]
				} else {
					return fmt.Errorf("worker '%s' not found\n\nAvailable workers:\n%s",
						targetName, workerNameList(workers))
				}
			}

			if flagJSON {
				return output.JSON(worker)
			}

			F := output.F
			F.Section(fmt.Sprintf("Worker: %s", worker.Name))

			statusStr := worker.Status
			switch worker.Status {
			case "healthy":
				statusStr = output.Green("healthy")
			case "failed":
				statusStr = output.Red("failed (env validation)")
			case "degraded":
				statusStr = output.Yellow("degraded")
			}

			F.KeyValue("Status", statusStr)
			F.KeyValue("Identity", worker.Identity)
			if worker.PID > 0 {
				F.KeyValue("PID", fmt.Sprint(worker.PID))
			}
			F.KeyValue("Task Queue", worker.TaskQueue)
			F.KeyValue("Host", worker.Host)
			F.KeyValue("Current Task", orDash(worker.CurrentTask))
			F.KeyValue("Last Activity", orDash(worker.LastActivity))
			if worker.Model != "" {
				F.KeyValue("Model", worker.Model)
			}

			// Environment section
			F.Println()
			F.Section("Environment")
			envChecks := getWorkerEnvChecks()
			for _, ec := range envChecks {
				if ec.found {
					F.Printf("  %s %s: set\n", output.Green("[OK]"), ec.name)
				} else {
					F.Printf("  %s %s: NOT SET — %s\n", output.Red("[FAIL]"), ec.name, ec.desc)
				}
			}

			// Logs
			if !noLogs {
				F.Println()
				showWorkerLogTail(F, logLines)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&logLines, "logs", 10, "Number of log tail lines to include")
	cmd.Flags().BoolVar(&noLogs, "no-logs", false, "Skip log section")
	return cmd
}

// gatherWorkerInfo collects worker info from API health + local inspection.
func gatherWorkerInfo(client *api.Client) []api.WorkerInfo {
	var workers []api.WorkerInfo

	// Try API /workers endpoint first (may not exist yet)
	var apiWorkers api.WorkersResponse
	if err := client.Get(ctx(), "/workers", &apiWorkers); err == nil && len(apiWorkers.Workers) > 0 {
		return apiWorkers.Workers
	}

	// Fallback: construct from /health + local inspection
	health, err := client.Health(ctx())
	if err != nil {
		return workers
	}

	cfg, _ := config.Load()
	installDir := ""
	if cfg != nil {
		installDir = cfg.EffectiveInstallDir()
	}

	if health.Worker.Count == 0 && !health.Worker.Connected {
		// No workers at all — check if local install exists
		if installDir != "" {
			if _, err := os.Stat(filepath.Join(installDir, "worker")); err == nil {
				workers = append(workers, api.WorkerInfo{
					Name:      "worker-1",
					Identity:  "local",
					Status:    "stopped",
					TaskQueue: health.Temporal.TaskQueue,
					EnvStatus: "unknown",
				})
			}
		}
		return workers
	}

	// Build worker entries from what we know
	for i := 0; i < max(health.Worker.Count, 1); i++ {
		name := "worker-1"
		if i > 0 {
			name = fmt.Sprintf("worker-%d", i+1)
		}

		w := api.WorkerInfo{
			Name:      name,
			Identity:  fmt.Sprintf("investigate-worker-%d", i+1),
			TaskQueue: health.Temporal.TaskQueue,
			Status:    "healthy",
			EnvStatus: "OK",
		}

		// Check env vars locally
		envChecks := getWorkerEnvChecks()
		var envErrors []string
		for _, ec := range envChecks {
			if !ec.found {
				envErrors = append(envErrors, ec.name)
			}
		}
		if len(envErrors) > 0 {
			w.Status = "failed"
			w.EnvStatus = fmt.Sprintf("%d errors", len(envErrors))
			w.EnvErrors = envErrors
		}

		// Check worker logs for errors
		if installDir != "" && w.Status == "healthy" {
			if hasRecentLogErrors(installDir) {
				w.Status = "degraded"
				w.EnvStatus = "log errors"
			}
		}

		// Try to detect model from local config
		if installDir != "" {
			w.Model = detectWorkerModel(installDir)
		}

		workers = append(workers, w)
	}

	return workers
}

type envCheck struct {
	name  string
	alts  []string
	desc  string
	found bool
}

func getWorkerEnvChecks() []envCheck {
	cfg, _ := config.Load()
	installDir := ""
	if cfg != nil {
		installDir = cfg.EffectiveInstallDir()
	}

	// Read .env file
	envVars := readEnvFile(filepath.Join(installDir, "worker", ".env"))

	checkEnv := func(name string, alts ...string) bool {
		if envVars[name] {
			return true
		}
		if os.Getenv(name) != "" {
			return true
		}
		for _, alt := range alts {
			if envVars[alt] || os.Getenv(alt) != "" {
				return true
			}
		}
		return false
	}

	return []envCheck{
		{"ANTHROPIC_API_KEY", nil, "required for LLM calls", checkEnv("ANTHROPIC_API_KEY")},
		{"GITHUB_TOKEN", []string{"GITHUB_PAT"}, "required for repo access", checkEnv("GITHUB_TOKEN", "GITHUB_PAT")},
		{"AWS_ACCESS_KEY_ID", nil, "required for AWS services (or use instance profile)", checkEnv("AWS_ACCESS_KEY_ID")},
		{"AWS_SECRET_ACCESS_KEY", nil, "required for AWS services (or use instance profile)", checkEnv("AWS_SECRET_ACCESS_KEY")},
	}
}

func readEnvFile(path string) map[string]bool {
	m := make(map[string]bool)
	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			m[parts[0]] = true
		}
	}
	return m
}

func hasRecentLogErrors(installDir string) bool {
	candidates := []string{
		filepath.Join(installDir, "logs", "worker.log"),
		filepath.Join(installDir, "worker", "worker.log"),
	}
	for _, logFile := range candidates {
		data, err := os.ReadFile(logFile)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		// Check last 20 lines
		start := 0
		if len(lines) > 20 {
			start = len(lines) - 20
		}
		for _, line := range lines[start:] {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "validation failed") || strings.Contains(lower, "critical") {
				return true
			}
		}
	}
	return false
}

func detectWorkerModel(installDir string) string {
	// Check worker .env for model config
	envFile := filepath.Join(installDir, "worker", ".env")
	f, err := os.Open(envFile)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "CLAUDE_MODEL=") || strings.HasPrefix(line, "MODEL_ID=") || strings.HasPrefix(line, "MODEL=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func showWorkerLogTail(F output.Formatter, lines int) {
	cfg, _ := config.Load()
	if cfg == nil {
		return
	}
	installDir := cfg.EffectiveInstallDir()
	candidates := []string{
		filepath.Join(installDir, "logs", "worker.log"),
		filepath.Join(installDir, "worker", "worker.log"),
	}
	for _, logFile := range candidates {
		data, err := os.ReadFile(logFile)
		if err != nil {
			continue
		}
		allLines := strings.Split(string(data), "\n")
		start := 0
		if len(allLines) > lines {
			start = len(allLines) - lines
		}
		F.Section(fmt.Sprintf("Recent Logs (last %d lines)", lines))
		for _, l := range allLines[start:] {
			if l != "" {
				F.Printf("  %s\n", l)
			}
		}
		return
	}
	F.Section("Logs")
	F.Info("No worker log file found")
}

func workerNameList(workers []api.WorkerInfo) string {
	if len(workers) == 0 {
		return "  (none)"
	}
	var sb strings.Builder
	for _, w := range workers {
		sb.WriteString(fmt.Sprintf("  - %s (%s)\n", w.Name, w.Status))
	}
	return sb.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
