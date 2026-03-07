package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Config is a subset of the full CLI config used by SetupLocal.
// Avoids importing the config package (which would create a cycle).
type Config struct {
	WorkerRepoURL   string
	APIRepoURL      string
	UIRepoURL       string
	DynamoDBTable   string
	DefaultModel    string
	TemporalPort    string
	TemporalUIPort  string
	APIPort         string
	UIPort          string
	Region          string
	ProviderEnvVars map[string]string // Provider-specific env vars (CLAUDE_CODE_USE_BEDROCK, CLAUDE_PROVIDER, etc.)
}

// LocalSetupResult holds the outcome of each setup step.
type LocalSetupResult struct {
	InstallDir string            `json:"installDir"`
	Token      string            `json:"token"`
	Steps      []LocalStepResult `json:"steps"`
	Success    bool              `json:"success"`
}

// LocalStepResult is one step in the setup process.
type LocalStepResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // ok, fail, skip
	Message string `json:"message,omitempty"`
}

// Printer abstracts formatted output so SetupLocal doesn't depend on the output package.
type Printer interface {
	Section(title string)
	Info(msg string)
	Success(msg string)
	Warning(msg string)
	Error(msg string)
	Printf(format string, args ...any)
}

// SetupLocal orchestrates a complete local RepoSwarm environment.
// Config values drive repo URLs, ports, table names, and model IDs.
func SetupLocal(env *Environment, installDir string, cfg *Config, printer Printer) (*LocalSetupResult, error) {
	result := &LocalSetupResult{InstallDir: installDir}

	// Initialize install log
	log := NewInstallLog(installDir)
	defer func() {
		log.Close()
		printer.Printf("\n  📄 Full install log: %s\n\n", log.Path())
	}()

	// Log config
	log.Section("Configuration")
	log.Info(fmt.Sprintf("installDir: %s", installDir))
	log.Info(fmt.Sprintf("region: %s", cfg.Region))
	log.Info(fmt.Sprintf("model: %s", cfg.DefaultModel))
	log.Info(fmt.Sprintf("apiPort: %s", cfg.APIPort))
	log.Info(fmt.Sprintf("uiPort: %s", cfg.UIPort))
	log.Info(fmt.Sprintf("temporalPort: %s", cfg.TemporalPort))
	log.Info(fmt.Sprintf("workerRepoURL: %s", cfg.WorkerRepoURL))
	log.Info(fmt.Sprintf("apiRepoURL: %s", cfg.APIRepoURL))
	log.Info(fmt.Sprintf("uiRepoURL: %s", cfg.UIRepoURL))

	// Step 0: Check prerequisites
	log.Section("Prerequisites")
	printer.Section("Checking prerequisites")
	if missing := env.MissingDeps(); len(missing) > 0 {
		for _, dep := range missing {
			printer.Error(fmt.Sprintf("Missing: %s", dep))
			log.Error(fmt.Sprintf("Missing prerequisite: %s", dep))
		}
		result.Steps = append(result.Steps, LocalStepResult{"prerequisites", "fail", "missing: " + strings.Join(missing, ", ")})
		return result, fmt.Errorf("missing prerequisites: %s — install them first", strings.Join(missing, ", "))
	}
	printer.Success("All prerequisites found")
	log.Success("All prerequisites found")
	result.Steps = append(result.Steps, LocalStepResult{"prerequisites", "ok", ""})

	// Generate a bearer token for local auth
	token, err := randomHex(32)
	if err != nil {
		return result, fmt.Errorf("generating token: %w", err)
	}
	result.Token = token

	// Step 1: Create directory structure
	printer.Section("Creating directory structure")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		result.Steps = append(result.Steps, LocalStepResult{"directories", "fail", err.Error()})
		return result, fmt.Errorf("creating install directory: %w", err)
	}
	printer.Success(fmt.Sprintf("Install directory: %s", installDir))
	result.Steps = append(result.Steps, LocalStepResult{"directories", "ok", installDir})

	// Step 2: Start all services (Temporal + API + Worker + UI via Docker Compose)
	log.Section("Docker Compose Setup")
	printer.Section("Starting all services (Docker Compose)")
	if err := setupDocker(installDir, cfg, token, printer, log); err != nil {
		result.Steps = append(result.Steps, LocalStepResult{"docker-compose", "fail", err.Error()})
		return result, fmt.Errorf("docker compose setup: %w", err)
	}
	result.Steps = append(result.Steps, LocalStepResult{"temporal", "ok", fmt.Sprintf("http://localhost:%s", cfg.TemporalUIPort)})
	result.Steps = append(result.Steps, LocalStepResult{"api", "ok", fmt.Sprintf("http://localhost:%s", cfg.APIPort)})
	result.Steps = append(result.Steps, LocalStepResult{"worker", "ok", ""})
	result.Steps = append(result.Steps, LocalStepResult{"ui", "ok", fmt.Sprintf("http://localhost:%s", cfg.UIPort)})

	// Step 3: Configure CLI
	printer.Section("Configuring CLI")
	if err := configureCLI(cfg, token); err != nil {
		result.Steps = append(result.Steps, LocalStepResult{"cli-config", "fail", err.Error()})
		return result, fmt.Errorf("CLI configuration: %w", err)
	}
	printer.Success("CLI configured for local API")
	result.Steps = append(result.Steps, LocalStepResult{"cli-config", "ok", ""})

	// Step 7: Verify
	printer.Section("Verifying services")
	verifyResult := verifyServices(cfg, printer)
	result.Steps = append(result.Steps, verifyResult)

	result.Success = verifyResult.Status != "fail"

	// Print summary
	printer.Section("Setup Complete")
	if result.Success {
		printer.Success("RepoSwarm local environment is running!")
	} else {
		printer.Warning("RepoSwarm started with some issues (see above)")
	}
	printer.Printf("\n")
	printer.Printf("  Temporal UI:  http://localhost:%s\n", cfg.TemporalUIPort)
	printer.Printf("  API Server:   http://localhost:%s\n", cfg.APIPort)
	printer.Printf("  UI:           http://localhost:%s\n", cfg.UIPort)
	printer.Printf("\n")
	printer.Printf("  API Token:    %s\n", token)
	printer.Printf("  Logs:         %s/*/*.log\n", installDir)
	printer.Printf("\n")
	printer.Printf("  Try:\n")
	printer.Printf("    reposwarm status\n")
	printer.Printf("    reposwarm repos add is-odd --url https://github.com/jonschlinkert/is-odd --source GitHub\n")
	printer.Printf("    reposwarm investigate is-odd\n")
	printer.Printf("\n")

	return result, nil
}


// killProcessOnPort attempts to kill any process listening on the given port.
func killProcessOnPort(port string) {
	// Use lsof to find the PID
	out, err := exec.Command("lsof", "-ti", fmt.Sprintf("tcp:%s", port)).Output()
	if err != nil || len(out) == 0 {
		return // nothing listening
	}
	for _, pidStr := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if pidStr != "" {
			exec.Command("kill", "-9", pidStr).Run()
		}
	}
	// Give it a moment to release the port
	time.Sleep(500 * time.Millisecond)
}
func setupDocker(installDir string, cfg *Config, token string, printer Printer, log *InstallLog) error {
	temporalDir := filepath.Join(installDir, "temporal")
	if err := os.MkdirAll(temporalDir, 0755); err != nil {
		return err
	}

	// Free ports that might be used by existing non-Docker services
	for _, port := range []string{cfg.APIPort, cfg.UIPort, cfg.TemporalPort, cfg.TemporalUIPort} {
		killProcessOnPort(port)
	}

	// Stop any existing compose stack in this directory
	if _, err := os.Stat(filepath.Join(temporalDir, "docker-compose.yml")); err == nil {
		printer.Info("Stopping existing containers...")
		log.RunCmd(temporalDir, "docker", "compose", "down")
	}

	composePath := filepath.Join(temporalDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(TemporalComposeLocal()), 0644); err != nil {
		return fmt.Errorf("writing docker-compose.yml: %w", err)
	}
	printer.Info("Wrote docker-compose.yml")
	log.Info("Wrote docker-compose.yml to " + composePath)

	// Write .env file with token, ports, and passthrough env vars
	envVars := []string{
		fmt.Sprintf("API_BEARER_TOKEN=%s", token),
		fmt.Sprintf("API_PORT=%s", cfg.APIPort),
		fmt.Sprintf("UI_PORT=%s", cfg.UIPort),
	}
	// Pass through LLM and git provider env vars from host
	for _, key := range []string{
		"ANTHROPIC_API_KEY", "CLAUDE_CODE_USE_BEDROCK", "AWS_REGION",
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"AWS_BEARER_TOKEN_BEDROCK", "AWS_PROFILE",
		"ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL",
		"GITHUB_TOKEN", "GITLAB_TOKEN",
	} {
		if v := os.Getenv(key); v != "" {
			envVars = append(envVars, fmt.Sprintf("%s=%s", key, v))
		}
	}
	envPath := filepath.Join(temporalDir, ".env")
	if err := os.WriteFile(envPath, []byte(strings.Join(envVars, "\n")+"\n"), 0600); err != nil {
		return fmt.Errorf("writing .env: %w", err)
	}
	printer.Info("Wrote .env")
	log.Info("Wrote .env to " + envPath)

	// docker compose up -d
	out, err := log.RunCmd(temporalDir, "docker", "compose", "up", "-d")
	if err != nil {
		return fmt.Errorf("docker compose up failed: %w\n%s", err, string(out))
	}
	printer.Info("Docker containers starting...")

	// Wait for Temporal to be ready (up to 60s)
	printer.Info("Waiting for Temporal to be ready (first run may take up to 5 minutes for schema setup)...")
	log.Info("Waiting for Temporal on port " + cfg.TemporalUIPort)
	if err := waitForHTTP(fmt.Sprintf("http://localhost:%s/api/v1/namespaces", cfg.TemporalUIPort), 300*time.Second); err != nil {
		// Check container status for debugging
		statusOut, _ := log.RunCmd(temporalDir, "docker", "compose", "ps", "--format", "{{.Name}}\t{{.Status}}")
		return fmt.Errorf("temporal not ready after 300s: %w\nContainer status:\n%s", err, string(statusOut))
	}
	printer.Success("Temporal is ready")
	log.Success("Temporal is ready")

	// Wait for API to be ready
	printer.Info("Waiting for API server...")
	log.Info("Waiting for API on port " + cfg.APIPort)
	if err := waitForHTTP(fmt.Sprintf("http://localhost:%s/v1/health", cfg.APIPort), 120*time.Second); err != nil {
		statusOut, _ := log.RunCmd(temporalDir, "docker", "compose", "ps", "--format", "{{.Name}}\t{{.Status}}")
		logsOut, _ := log.RunCmd(temporalDir, "docker", "compose", "logs", "api", "--tail", "30")
		return fmt.Errorf("API not ready after 120s: %w\nContainer status:\n%s\nAPI logs:\n%s", err, string(statusOut), string(logsOut))
	}
	printer.Success("API server is ready")
	log.Success("API server is ready")

	// Wait for UI to be ready
	printer.Info("Waiting for UI...")
	log.Info("Waiting for UI on port " + cfg.UIPort)
	if err := waitForHTTP(fmt.Sprintf("http://localhost:%s", cfg.UIPort), 120*time.Second); err != nil {
		printer.Warning("UI not ready yet — may still be starting. Check: docker compose logs ui")
		log.Warning("UI not ready after 120s")
	} else {
		printer.Success("UI is ready")
		log.Success("UI is ready")
	}

	return nil
}

func setupAPI(installDir string, cfg *Config, token string, printer Printer, log *InstallLog) error {
	apiDir := filepath.Join(installDir, "api")

	// Clone
	if _, err := os.Stat(apiDir); os.IsNotExist(err) {
		printer.Info("Cloning API server...")
		log.Info("Cloning " + cfg.APIRepoURL)
		out, err := log.RunCmd(installDir, "git", "clone", cfg.APIRepoURL, "api")
		if err != nil {
			return fmt.Errorf("git clone failed: %w\n%s", err, string(out))
		}
	} else {
		printer.Info("API directory exists, skipping clone")
		log.Info("API directory exists, skipping clone")
	}

	// npm install
	printer.Info("Installing dependencies...")
	out, err := log.RunCmd(apiDir, "npm", "install")
	if err != nil {
		return fmt.Errorf("npm install failed: %w\n%s", err, string(out))
	}

	// npm run build
	printer.Info("Building...")
	out, err = log.RunCmd(apiDir, "npm", "run", "build")
	if err != nil {
		return fmt.Errorf("npm build failed: %w\n%s", err, string(out))
	}

	// Write .env
	envContent := fmt.Sprintf(`PORT=%s
TEMPORAL_SERVER_URL=localhost:%s
TEMPORAL_NAMESPACE=default
TEMPORAL_TASK_QUEUE=investigate-task-queue
AWS_REGION=%s
DYNAMODB_TABLE=%s
API_BEARER_TOKEN=%s
DYNAMODB_ENDPOINT=http://localhost:8000
AWS_ACCESS_KEY_ID=local
AWS_SECRET_ACCESS_KEY=local
`, cfg.APIPort, cfg.TemporalPort, cfg.Region, cfg.DynamoDBTable, token)

	if err := os.WriteFile(filepath.Join(apiDir, ".env"), []byte(envContent), 0600); err != nil {
		return fmt.Errorf("writing .env: %w", err)
	}
	log.Info("Wrote .env file")

	// Kill any existing process on the API port
	killProcessOnPort(cfg.APIPort)

	// Start API in background
	printer.Info("Starting API server...")
	log.Info("Starting API on port " + cfg.APIPort)
	logFile, err := os.Create(filepath.Join(apiDir, "api.log"))
	if err != nil {
		return fmt.Errorf("creating log file: %w", err)
	}

	startCmd := exec.Command("npm", "start")
	startCmd.Dir = apiDir
	startCmd.Stdout = logFile
	startCmd.Stderr = logFile
	apiEnv := []string{
		fmt.Sprintf("PORT=%s", cfg.APIPort),
		fmt.Sprintf("TEMPORAL_SERVER_URL=localhost:%s", cfg.TemporalPort),
		"TEMPORAL_NAMESPACE=default",
		"TEMPORAL_TASK_QUEUE=investigate-task-queue",
		fmt.Sprintf("AWS_REGION=%s", cfg.Region),
		fmt.Sprintf("DYNAMODB_TABLE=%s", cfg.DynamoDBTable),
		"DYNAMODB_ENDPOINT=http://localhost:8000",
		fmt.Sprintf("API_BEARER_TOKEN=%s", token),
		"AWS_ACCESS_KEY_ID=local",
		"AWS_SECRET_ACCESS_KEY=local",
	}
	startCmd.Env = append(os.Environ(), apiEnv...)
	log.Env(apiEnv)
	if err := startCmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting API: %w", err)
	}
	logFile.Close()

	// Write PID file for later management
	pidFile := filepath.Join(apiDir, "api.pid")
	os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", startCmd.Process.Pid)), 0644)

	// Wait for API
	printer.Info("Waiting for API to be ready...")
	if err := waitForHTTP(fmt.Sprintf("http://localhost:%s/v1/health", cfg.APIPort), 30*time.Second); err != nil {
		return fmt.Errorf("API not ready after 30s: %w", err)
	}
	printer.Success("API server is ready")
	log.Success("API server is ready (PID " + fmt.Sprintf("%d", startCmd.Process.Pid) + ")")
	return nil
}

func setupWorker(installDir string, cfg *Config, printer Printer, log *InstallLog) error {
	workerDir := filepath.Join(installDir, "worker")

	// Clone
	if _, err := os.Stat(workerDir); os.IsNotExist(err) {
		printer.Info("Cloning worker...")
		log.Info("Cloning " + cfg.WorkerRepoURL)
		out, err := log.RunCmd(installDir, "git", "clone", cfg.WorkerRepoURL, "worker")
		if err != nil {
			return fmt.Errorf("git clone failed: %w\n%s", err, string(out))
		}
	} else {
		printer.Info("Worker directory exists, skipping clone")
		log.Info("Worker directory exists, skipping clone")
	}

	// Create venv
	printer.Info("Creating Python virtual environment...")
	out, err := log.RunCmd(workerDir, "python3", "-m", "venv", ".venv")
	if err != nil {
		return fmt.Errorf("venv creation failed: %w\n%s", err, string(out))
	}

	// Install Python dependencies (supports both requirements.txt and pyproject.toml)
	printer.Info("Installing Python dependencies...")
	pipPath := filepath.Join(workerDir, ".venv", "bin", "pip")
	reqFile := filepath.Join(workerDir, "requirements.txt")
	if _, err := os.Stat(reqFile); err == nil {
		out, err = log.RunCmd(workerDir, pipPath, "install", "-r", "requirements.txt")
	} else {
		out, err = log.RunCmd(workerDir, pipPath, "install", "-e", ".")
	}
	if err != nil {
		return fmt.Errorf("pip install failed: %w\n%s", err, string(out))
	}

	// Write .env
	envContent := fmt.Sprintf(`TEMPORAL_SERVER_URL=localhost:%s
TEMPORAL_NAMESPACE=default
TEMPORAL_TASK_QUEUE=investigate-task-queue
AWS_REGION=%s
DYNAMODB_TABLE=%s
DYNAMODB_TABLE_NAME=%s
DYNAMODB_ENDPOINT=http://localhost:8000
DEFAULT_MODEL=%s
`, cfg.TemporalPort, cfg.Region, cfg.DynamoDBTable, cfg.DynamoDBTable, cfg.DefaultModel)

	// Append provider env vars (CLAUDE_CODE_USE_BEDROCK, CLAUDE_PROVIDER, AWS_BEARER_TOKEN_BEDROCK, etc.)
	for k, v := range cfg.ProviderEnvVars {
		envContent += fmt.Sprintf("%s=%s\n", k, v)
	}

	if err := os.WriteFile(filepath.Join(workerDir, ".env"), []byte(envContent), 0600); err != nil {
		return fmt.Errorf("writing .env: %w", err)
	}
	log.Info("Wrote worker .env file")

	// Kill any existing worker process
	killProcessOnPort(cfg.TemporalPort) // workers connect to temporal, not a specific port

	// Start worker in background
	printer.Info("Starting worker...")
	logFile, err := os.Create(filepath.Join(workerDir, "worker.log"))
	if err != nil {
		return fmt.Errorf("creating log file: %w", err)
	}

	pythonPath := filepath.Join(workerDir, ".venv", "bin", "python")
	// Try src.worker first (pyproject.toml layout), fall back to worker.main
	workerModule := "worker.main"
	if _, err := os.Stat(filepath.Join(workerDir, "src")); err == nil {
		workerModule = "src.worker"
	}
	log.Info("Worker module: " + workerModule)
	startCmd := exec.Command(pythonPath, "-m", workerModule)
	startCmd.Dir = workerDir
	startCmd.Stdout = logFile
	startCmd.Stderr = logFile
	workerEnv := []string{
		fmt.Sprintf("TEMPORAL_SERVER_URL=localhost:%s", cfg.TemporalPort),
		"TEMPORAL_NAMESPACE=default",
		"TEMPORAL_TASK_QUEUE=investigate-task-queue",
		fmt.Sprintf("AWS_REGION=%s", cfg.Region),
		fmt.Sprintf("DYNAMODB_TABLE=%s", cfg.DynamoDBTable),
		fmt.Sprintf("DYNAMODB_TABLE_NAME=%s", cfg.DynamoDBTable),
		"DYNAMODB_ENDPOINT=http://localhost:8000",
		fmt.Sprintf("DEFAULT_MODEL=%s", cfg.DefaultModel),
	}
	// Add provider env vars (CLAUDE_CODE_USE_BEDROCK, CLAUDE_PROVIDER, AWS_BEARER_TOKEN_BEDROCK, etc.)
	for k, v := range cfg.ProviderEnvVars {
		workerEnv = append(workerEnv, fmt.Sprintf("%s=%s", k, v))
	}
	startCmd.Env = append(os.Environ(), workerEnv...)
	log.Env(workerEnv)
	if err := startCmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting worker: %w", err)
	}
	logFile.Close()

	pidFile := filepath.Join(workerDir, "worker.pid")
	os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", startCmd.Process.Pid)), 0644)

	printer.Success("Worker started")
	log.Success("Worker started (PID " + fmt.Sprintf("%d", startCmd.Process.Pid) + ")")
	return nil
}

func setupUI(installDir string, cfg *Config, printer Printer, log *InstallLog) error {
	uiDir := filepath.Join(installDir, "ui")

	// Clone
	if _, err := os.Stat(uiDir); os.IsNotExist(err) {
		printer.Info("Cloning UI...")
		log.Info("Cloning " + cfg.UIRepoURL)
		out, err := log.RunCmd(installDir, "git", "clone", cfg.UIRepoURL, "ui")
		if err != nil {
			return fmt.Errorf("git clone failed: %w\n%s", err, string(out))
		}
	} else {
		printer.Info("UI directory exists, skipping clone")
		log.Info("UI directory exists, skipping clone")
	}

	// npm install
	printer.Info("Installing dependencies...")
	out, err := log.RunCmd(uiDir, "npm", "install")
	if err != nil {
		return fmt.Errorf("npm install failed: %w\n%s", err, string(out))
	}

	// Write .env.local
	envContent := fmt.Sprintf("NEXT_PUBLIC_API_URL=http://localhost:%s\n", cfg.APIPort)
	if err := os.WriteFile(filepath.Join(uiDir, ".env.local"), []byte(envContent), 0644); err != nil {
		return fmt.Errorf("writing .env.local: %w", err)
	}
	log.Info("Wrote UI .env.local")

	// Kill any existing process on the UI port
	killProcessOnPort(cfg.UIPort)

	// Start UI in background
	printer.Info("Starting UI dev server...")
	log.Info("Starting UI on port " + cfg.UIPort)
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

	// Wait for UI
	printer.Info("Waiting for UI to be ready...")
	if err := waitForHTTP(fmt.Sprintf("http://localhost:%s", cfg.UIPort), 30*time.Second); err != nil {
		printer.Warning("UI not responding yet — it may still be compiling (check ui/ui.log)")
		log.Warning("UI not responding after 30s")
		return nil // Non-fatal
	}
	printer.Success("UI is ready")
	log.Success("UI is ready (PID " + fmt.Sprintf("%d", startCmd.Process.Pid) + ")")
	return nil
}

func configureCLI(cfg *Config, token string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configDir := filepath.Join(home, ".reposwarm")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}
	configContent := fmt.Sprintf(`{
  "apiUrl": "http://localhost:%s/v1",
  "apiToken": "%s",
  "region": "us-east-1",
  "defaultModel": "%s",
  "chunkSize": 10,
  "outputFormat": "pretty"
}
`, cfg.APIPort, token, cfg.DefaultModel)
	return os.WriteFile(filepath.Join(configDir, "config.json"), []byte(configContent), 0600)
}

func verifyServices(cfg *Config, printer Printer) LocalStepResult {
	checks := []struct {
		name string
		url  string
	}{
		{"Temporal", fmt.Sprintf("http://localhost:%s/api/v1/namespaces", cfg.TemporalUIPort)},
		{"DynamoDB Local", "http://localhost:8000"},
		{"API", fmt.Sprintf("http://localhost:%s/v1/health", cfg.APIPort)},
		{"UI", fmt.Sprintf("http://localhost:%s", cfg.UIPort)},
	}

	allOK := true
	var messages []string
	for _, c := range checks {
		resp, err := http.Get(c.url)
		if err != nil {
			printer.Warning(fmt.Sprintf("%s: not responding (%s)", c.name, err))
			messages = append(messages, fmt.Sprintf("%s: fail", c.name))
			allOK = false
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 500 {
			printer.Success(fmt.Sprintf("%s: healthy", c.name))
			messages = append(messages, fmt.Sprintf("%s: ok", c.name))
		} else {
			printer.Warning(fmt.Sprintf("%s: HTTP %d", c.name, resp.StatusCode))
			messages = append(messages, fmt.Sprintf("%s: HTTP %d", c.name, resp.StatusCode))
			allOK = false
		}
	}

	status := "ok"
	if !allOK {
		status = "fail"
	}
	return LocalStepResult{"verify", status, strings.Join(messages, "; ")}
}

// TemporalComposeLocal returns the docker-compose YAML for local development.
// Uses postgres instead of the deprecated sqlite driver.
func TemporalComposeLocal() string {
	return `services:
  postgres:
    image: postgres:16-alpine
    ports:
      - "5432:5432"
    environment:
      POSTGRES_USER: temporal
      POSTGRES_PASSWORD: temporal
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U temporal"]
      interval: 5s
      timeout: 5s
      retries: 10
    volumes:
      - temporal-data:/var/lib/postgresql/data

  temporal:
    image: temporalio/auto-setup:latest
    ports:
      - "7233:7233"
    environment:
      - DB=postgres12
      - DB_PORT=5432
      - POSTGRES_USER=temporal
      - POSTGRES_PWD=temporal
      - POSTGRES_SEEDS=postgres
      - DYNAMIC_CONFIG_FILE_PATH=config/dynamicconfig/docker.yaml
      - SKIP_DEFAULT_NAMESPACE_CREATION=false
    depends_on:
      postgres:
        condition: service_healthy

  temporal-ui:
    image: temporalio/ui:latest
    ports:
      - "8233:8080"
    environment:
      - TEMPORAL_ADDRESS=temporal:7233
    depends_on:
      - temporal

  dynamodb-local:
    image: amazon/dynamodb-local:latest
    ports:
      - "8000:8000"
    command: ["-jar", "DynamoDBLocal.jar", "-sharedDb"]
    volumes:
      - dynamodb-data:/home/dynamodblocal/data

  api:
    image: ghcr.io/reposwarm/api:latest
    ports:
      - "${API_PORT:-3000}:3000"
    environment:
      - PORT=3000
      - API_BEARER_TOKEN=${API_BEARER_TOKEN}
      - TEMPORAL_SERVER_URL=temporal:7233
      - TEMPORAL_HTTP_URL=http://temporal-ui:8080
      - TEMPORAL_NAMESPACE=default
      - AWS_REGION=${AWS_REGION:-us-east-1}
      - DYNAMODB_ENDPOINT=http://dynamodb-local:8000
      - DYNAMODB_TABLE=${DYNAMODB_TABLE:-reposwarm-cache}
      - REPOSWARM_INSTALL_DIR=/data
    volumes:
      - config-data:/data
    depends_on:
      temporal:
        condition: service_started
      temporal-ui:
        condition: service_started
      dynamodb-local:
        condition: service_started
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:3000/v1/health"]
      interval: 10s
      timeout: 5s
      retries: 5

  worker:
    image: ghcr.io/reposwarm/worker:latest
    env_file:
      - path: ./worker.env
        required: false
    environment:
      - TEMPORAL_SERVER_URL=temporal:7233
      - TEMPORAL_NAMESPACE=default
      - TEMPORAL_TASK_QUEUE=investigate-task-queue
      - AWS_REGION=${AWS_REGION:-us-east-1}
      - DYNAMODB_ENDPOINT=http://dynamodb-local:8000
      - DYNAMODB_TABLE=${DYNAMODB_TABLE:-reposwarm-cache}
      - LOCAL_TESTING=true
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:-}
      - CLAUDE_CODE_USE_BEDROCK=${CLAUDE_CODE_USE_BEDROCK:-}
      - AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID:-}
      - AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY:-}
      - AWS_SESSION_TOKEN=${AWS_SESSION_TOKEN:-}
      - AWS_BEARER_TOKEN_BEDROCK=${AWS_BEARER_TOKEN_BEDROCK:-}
      - AWS_PROFILE=${AWS_PROFILE:-}
      - ANTHROPIC_BASE_URL=${ANTHROPIC_BASE_URL:-}
      - ANTHROPIC_MODEL=${ANTHROPIC_MODEL:-}
      - GITHUB_TOKEN=${GITHUB_TOKEN:-}
      - GITLAB_TOKEN=${GITLAB_TOKEN:-}
    volumes:
      - config-data:/data
    depends_on:
      - temporal
      - dynamodb-local

  ui:
    image: ghcr.io/reposwarm/ui:latest
    ports:
      - "${UI_PORT:-3001}:3000"
    environment:
      - TEMPORAL_SERVER_URL=http://temporal-ui:8080
      - AWS_REGION=${AWS_REGION:-us-east-1}
      - DYNAMODB_ENDPOINT=http://dynamodb-local:8000
      - DYNAMODB_CACHE_TABLE=${DYNAMODB_TABLE:-reposwarm-cache}
    depends_on:
      api:
        condition: service_healthy

  askbox:
    image: ghcr.io/reposwarm/askbox:latest
    environment:
      - ASKBOX_ADAPTER=${ASKBOX_ADAPTER:-claude-agent-sdk}
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:-}
      - CLAUDE_CODE_USE_BEDROCK=${CLAUDE_CODE_USE_BEDROCK:-}
      - AWS_REGION=${AWS_REGION:-}
      - AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID:-}
      - AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY:-}
      - AWS_SESSION_TOKEN=${AWS_SESSION_TOKEN:-}
      - AWS_PROFILE=${AWS_PROFILE:-}
      - AWS_BEARER_TOKEN_BEDROCK=${AWS_BEARER_TOKEN_BEDROCK:-}
      - ANTHROPIC_BASE_URL=${ANTHROPIC_BASE_URL:-}
      - LITELLM_API_URL=${LITELLM_API_URL:-}
      - LITELLM_API_KEY=${LITELLM_API_KEY:-}
      - MODEL_ID=${MODEL_ID:-}
    volumes:
      - askbox-output:/output
      - askbox-arch-hub:/tmp/arch-hub
    profiles:
      - ask

volumes:
  temporal-data:
  dynamodb-data:
  config-data:
  askbox-output:
  askbox-arch-hub:
`
}

func waitForHTTP(url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for %s", url)
		case <-ticker.C:
			resp, err := client.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode < 500 {
					return nil
				}
			}
		}
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
