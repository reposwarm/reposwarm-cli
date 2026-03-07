package bootstrap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ServiceStatus holds runtime info about a locally managed service.
type ServiceStatus struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	PID     int    `json:"pid,omitempty"`
	Port    string `json:"port,omitempty"`
}

// serviceInfo maps service names to their subdirectory, start command, and port config key.
type serviceInfo struct {
	Dir      string   // subdirectory under install dir
	PIDFile  string   // PID file name
	Port     func(*Config) string
}

var serviceMap = map[string]serviceInfo{
	"api":      {Dir: "api", PIDFile: "api.pid", Port: func(c *Config) string { return c.APIPort }},
	"worker":   {Dir: "worker", PIDFile: "worker.pid", Port: func(c *Config) string { return "" }},
	"ui":       {Dir: "ui", PIDFile: "ui.pid", Port: func(c *Config) string { return c.UIPort }},
	"temporal": {Dir: "temporal", PIDFile: "", Port: func(c *Config) string { return c.TemporalPort }},
}

// serviceRepoURL returns the git clone URL for a service, or "" if unknown.
func serviceRepoURL(service string, cfg *Config) string {
	switch service {
	case "api":
		if cfg.APIRepoURL != "" {
			return cfg.APIRepoURL
		}
		return "https://github.com/reposwarm/reposwarm-api.git"
	case "worker":
		if cfg.WorkerRepoURL != "" {
			return cfg.WorkerRepoURL
		}
		return "https://github.com/reposwarm/reposwarm.git"
	case "ui":
		if cfg.UIRepoURL != "" {
			return cfg.UIRepoURL
		}
		return "https://github.com/reposwarm/reposwarm-ui.git"
	default:
		return ""
	}
}

// IsLocalInstall checks whether a local installation exists at the given directory.
func IsLocalInstall(installDir string) bool {
	// Check if at least the api or worker subdirectory exists
	for _, sub := range []string{"api", "worker"} {
		if _, err := os.Stat(filepath.Join(installDir, sub)); err == nil {
			return true
		}
	}
	return false
}

// LocalServiceStatus returns the status of a locally installed service.
func LocalServiceStatus(installDir string, service string, cfg *Config) (*ServiceStatus, error) {
	info, ok := serviceMap[service]
	if !ok {
		return nil, fmt.Errorf("unknown service: %s", service)
	}

	status := &ServiceStatus{Name: service}
	if info.Port != nil {
		status.Port = info.Port(cfg)
	}

	if service == "temporal" {
		// Check docker compose
		temporalDir := filepath.Join(installDir, ComposeSubDir)
		cmd := exec.Command("docker", "compose", "ps", "--format", "{{.State}}")
		cmd.Dir = temporalDir
		out, err := cmd.CombinedOutput()
		if err == nil && strings.Contains(string(out), "running") {
			status.Running = true
		}
		return status, nil
	}

	// Check PID file
	svcDir := filepath.Join(installDir, info.Dir)
	pidPath := filepath.Join(svcDir, info.PIDFile)
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		return status, nil // Not running (no PID file)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		return status, nil
	}

	// Check if process is alive
	proc, err := os.FindProcess(pid)
	if err != nil {
		return status, nil
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// Process is dead, clean up stale PID file
		os.Remove(pidPath)
		return status, nil
	}

	status.Running = true
	status.PID = pid
	return status, nil
}

// LocalStart starts a service locally by spawning the process directly.
// This is the fallback when the API server is unreachable.
func LocalStart(installDir string, service string, cfg *Config) error {
	info, ok := serviceMap[service]
	if !ok {
		return fmt.Errorf("unknown service: %s", service)
	}

	svcDir := filepath.Join(installDir, info.Dir)
	if _, err := os.Stat(svcDir); os.IsNotExist(err) {
		// Try to auto-clone the missing service
		repoURL := serviceRepoURL(service, cfg)
		if repoURL != "" {
			fmt.Printf("  ℹ %s not found, cloning from %s...\n", service, repoURL)
			os.MkdirAll(installDir, 0755)
			cloneCmd := exec.Command("git", "clone", repoURL, info.Dir)
			cloneCmd.Dir = installDir
			if out, err := cloneCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("auto-clone failed: %w\n%s\nRun 'reposwarm new --local' to set up manually", err, string(out))
			}
			// Run npm install for Node.js services
			if service == "api" || service == "ui" {
				fmt.Printf("  ℹ Installing %s dependencies...\n", service)
				npmCmd := exec.Command("npm", "install")
				npmCmd.Dir = svcDir
				if out, err := npmCmd.CombinedOutput(); err != nil {
					return fmt.Errorf("npm install failed: %w\n%s", err, string(out))
				}
				// Build for API
				if service == "api" {
					fmt.Printf("  ℹ Building %s...\n", service)
					buildCmd := exec.Command("npm", "run", "build")
					buildCmd.Dir = svcDir
					if out, err := buildCmd.CombinedOutput(); err != nil {
						return fmt.Errorf("npm build failed: %w\n%s", err, string(out))
					}
				}
			}
		} else {
			return fmt.Errorf("%s directory not found at %s (run 'reposwarm new --local' first)", service, svcDir)
		}
	}

	// Check if already running
	status, _ := LocalServiceStatus(installDir, service, cfg)
	if status != nil && status.Running {
		return fmt.Errorf("%s is already running (PID %d)", service, status.PID)
	}

	switch service {
	case "temporal":
		return localStartTemporal(installDir, cfg)
	case "api":
		return localStartAPI(installDir, cfg)
	case "worker":
		return localStartWorker(installDir, cfg)
	case "ui":
		return localStartUI(installDir, cfg)
	default:
		return fmt.Errorf("don't know how to start %s locally", service)
	}
}

// LocalStop stops a locally running service.
func LocalStop(installDir string, service string, cfg *Config) error {
	if service == "temporal" {
		temporalDir := filepath.Join(installDir, ComposeSubDir)
		cmd := exec.Command("docker", "compose", "stop")
		cmd.Dir = temporalDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("docker compose stop failed: %w\n%s", err, string(out))
		}
		return nil
	}

	info, ok := serviceMap[service]
	if !ok {
		return fmt.Errorf("unknown service: %s", service)
	}

	svcDir := filepath.Join(installDir, info.Dir)
	pidPath := filepath.Join(svcDir, info.PIDFile)
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		return fmt.Errorf("%s is not running (no PID file)", service)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		os.Remove(pidPath)
		return fmt.Errorf("invalid PID file for %s", service)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidPath)
		return fmt.Errorf("%s process %d not found", service, pid)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// Try SIGKILL
		proc.Signal(syscall.SIGKILL)
	}
	os.Remove(pidPath)
	return nil
}

// LocalRestart stops then starts a service.
func LocalRestart(installDir string, service string, cfg *Config) error {
	// Check if this is a Docker Compose install
	composePath := filepath.Join(installDir, ComposeSubDir, "docker-compose.yml")
	if _, err := os.Stat(composePath); err == nil {
		// Docker install: use docker compose up -d --force-recreate
		// (not "restart" — restart doesn't re-read env_file changes)
		composeDir := filepath.Join(installDir, ComposeSubDir)
		cmd := exec.Command("docker", "compose", "up", "-d", "--force-recreate", service)
		cmd.Dir = composeDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("docker compose recreate failed: %w\n%s", err, string(out))
		}
		return nil
	}

	// Source-based install: stop then start
	LocalStop(installDir, service, cfg)
	time.Sleep(1 * time.Second)
	return LocalStart(installDir, service, cfg)
}

func localStartTemporal(installDir string, cfg *Config) error {
	temporalDir := filepath.Join(installDir, ComposeSubDir)
	composePath := filepath.Join(temporalDir, "docker-compose.yml")
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		return fmt.Errorf("docker-compose.yml not found at %s", composePath)
	}

	killProcessOnPort(cfg.TemporalPort)

	cmd := exec.Command("docker", "compose", "up", "-d")
	cmd.Dir = temporalDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose up failed: %w\n%s", err, string(out))
	}
	return nil
}

func localStartAPI(installDir string, cfg *Config) error {
	apiDir := filepath.Join(installDir, "api")

	killProcessOnPort(cfg.APIPort)

	logFile, err := os.Create(filepath.Join(apiDir, "api.log"))
	if err != nil {
		return fmt.Errorf("creating log file: %w", err)
	}

	// Read token from config
	token := readTokenFromConfig(installDir)

	startCmd := exec.Command("npm", "start")
	startCmd.Dir = apiDir
	startCmd.Stdout = logFile
	startCmd.Stderr = logFile
	startCmd.Env = append(os.Environ(),
		fmt.Sprintf("PORT=%s", orDefaultStr(cfg.APIPort, "3000")),
		fmt.Sprintf("TEMPORAL_SERVER_URL=localhost:%s", orDefaultStr(cfg.TemporalPort, "7233")),
		"TEMPORAL_NAMESPACE=default",
		"TEMPORAL_TASK_QUEUE=investigate-task-queue",
		fmt.Sprintf("AWS_REGION=%s", orDefaultStr(cfg.Region, "us-east-1")),
		fmt.Sprintf("DYNAMODB_TABLE=%s", orDefaultStr(cfg.DynamoDBTable, "reposwarm-cache")),
		"DYNAMODB_ENDPOINT=http://localhost:8000",
		fmt.Sprintf("API_BEARER_TOKEN=%s", token),
		"AWS_ACCESS_KEY_ID=local",
		"AWS_SECRET_ACCESS_KEY=local",
	)
	if err := startCmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting API: %w", err)
	}
	logFile.Close()

	pidFile := filepath.Join(apiDir, "api.pid")
	os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", startCmd.Process.Pid)), 0644)

	// Wait for API to be ready
	if err := waitForHTTP(fmt.Sprintf("http://localhost:%s/v1/health", orDefaultStr(cfg.APIPort, "3000")), 30*time.Second); err != nil {
		return fmt.Errorf("API started but not responding after 30s (check %s/api.log)", apiDir)
	}
	return nil
}

func localStartWorker(installDir string, cfg *Config) error {
	workerDir := filepath.Join(installDir, "worker")

	logFile, err := os.Create(filepath.Join(workerDir, "worker.log"))
	if err != nil {
		return fmt.Errorf("creating log file: %w", err)
	}

	pythonPath := filepath.Join(workerDir, ".venv", "bin", "python")
	if _, err := os.Stat(pythonPath); os.IsNotExist(err) {
		return fmt.Errorf("Python venv not found at %s (run 'reposwarm new --local' first)", pythonPath)
	}

	workerModule := "worker.main"
	if _, err := os.Stat(filepath.Join(workerDir, "src")); err == nil {
		workerModule = "src.worker"
	}

	startCmd := exec.Command(pythonPath, "-m", workerModule)
	startCmd.Dir = workerDir
	startCmd.Stdout = logFile
	startCmd.Stderr = logFile
	startCmd.Env = append(os.Environ(),
		fmt.Sprintf("TEMPORAL_SERVER_URL=localhost:%s", orDefaultStr(cfg.TemporalPort, "7233")),
		"TEMPORAL_NAMESPACE=default",
		"TEMPORAL_TASK_QUEUE=investigate-task-queue",
		fmt.Sprintf("AWS_REGION=%s", orDefaultStr(cfg.Region, "us-east-1")),
		fmt.Sprintf("DYNAMODB_TABLE=%s", orDefaultStr(cfg.DynamoDBTable, "reposwarm-cache")),
		fmt.Sprintf("DYNAMODB_TABLE_NAME=%s", orDefaultStr(cfg.DynamoDBTable, "reposwarm-cache")),
		"DYNAMODB_ENDPOINT=http://localhost:8000",
		fmt.Sprintf("DEFAULT_MODEL=%s", orDefaultStr(cfg.DefaultModel, "us.anthropic.claude-sonnet-4-6")),
	)
	// Add provider-specific env vars (CLAUDE_CODE_USE_BEDROCK, CLAUDE_PROVIDER, etc.)
	for k, v := range cfg.ProviderEnvVars {
		startCmd.Env = append(startCmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	if err := startCmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting worker: %w", err)
	}
	logFile.Close()

	pidFile := filepath.Join(workerDir, "worker.pid")
	os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", startCmd.Process.Pid)), 0644)
	return nil
}

func localStartUI(installDir string, cfg *Config) error {
	uiDir := filepath.Join(installDir, "ui")

	killProcessOnPort(orDefaultStr(cfg.UIPort, "3001"))

	logFile, err := os.Create(filepath.Join(uiDir, "ui.log"))
	if err != nil {
		return fmt.Errorf("creating log file: %w", err)
	}

	startCmd := exec.Command("npm", "run", "dev")
	startCmd.Dir = uiDir
	startCmd.Stdout = logFile
	startCmd.Stderr = logFile
	if err := startCmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting UI: %w", err)
	}
	logFile.Close()

	pidFile := filepath.Join(uiDir, "ui.pid")
	os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", startCmd.Process.Pid)), 0644)
	return nil
}

// readTokenFromConfig reads the API token from the CLI config file.
func readTokenFromConfig(installDir string) string {
	// Try reading from ~/.reposwarm/config.json
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".reposwarm", "config.json"))
	if err != nil {
		return ""
	}
	// Simple extraction without importing encoding/json to avoid heavy deps
	// Look for "apiToken": "..."
	s := string(data)
	idx := strings.Index(s, `"apiToken"`)
	if idx < 0 {
		return ""
	}
	rest := s[idx:]
	q1 := strings.Index(rest, `: "`)
	if q1 < 0 {
		return ""
	}
	rest = rest[q1+3:]
	q2 := strings.Index(rest, `"`)
	if q2 < 0 {
		return ""
	}
	return rest[:q2]
}

func orDefaultStr(val, def string) string {
	if val == "" {
		return def
	}
	return val
}
