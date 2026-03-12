package bootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ComposeSubDir is the subdirectory within installDir for docker-compose.yml.
const ComposeSubDir = "temporal"

// DockerService represents a Docker Compose service status.
type DockerService struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	State   string `json:"State"`
	Status  string `json:"Status"`
	Health  string `json:"Health"`
	Ports   string `json:"Publishers"`
}

// dockerServiceJSON is the raw JSON from docker compose ps --format json.
type dockerServiceJSON struct {
	Name       string `json:"Name"`
	Service    string `json:"Service"`
	State      string `json:"State"`
	Status     string `json:"Status"`
	Health     string `json:"Health"`
	Publishers []struct {
		URL           string `json:"URL"`
		TargetPort    int    `json:"TargetPort"`
		PublishedPort int    `json:"PublishedPort"`
		Protocol      string `json:"Protocol"`
	} `json:"Publishers"`
}

// IsDockerInstall checks if the install dir has a docker-compose.yml.
func IsDockerInstall(installDir string) bool {
	composePath := filepath.Join(installDir, ComposeSubDir, "docker-compose.yml")
	_, err := os.Stat(composePath)
	return err == nil
}

// DockerComposeServices returns the status of all Docker Compose services.
func DockerComposeServices(installDir string) ([]DockerService, error) {
	composeDir := filepath.Join(installDir, ComposeSubDir)
	cmd := exec.Command("docker", "compose", "ps", "--format", "json", "-a")
	cmd.Dir = composeDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker compose ps failed: %w\n%s", err, string(out))
	}

	// docker compose ps --format json outputs one JSON object per line (NDJSON)
	var services []DockerService
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw dockerServiceJSON
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		svc := DockerService{
			Name:    raw.Name,
			Service: raw.Service,
			State:   raw.State,
			Status:  raw.Status,
			Health:  raw.Health,
		}
		// Format ports
		var ports []string
		for _, p := range raw.Publishers {
			if p.PublishedPort > 0 {
				ports = append(ports, fmt.Sprintf("%d→%d", p.PublishedPort, p.TargetPort))
			}
		}
		svc.Ports = strings.Join(ports, ", ")
		services = append(services, svc)
	}
	return services, nil
}

// DockerServiceEnv reads env vars from a running Docker Compose service.
func DockerServiceEnv(installDir, service string) (map[string]string, error) {
	composeDir := filepath.Join(installDir, ComposeSubDir)
	cmd := exec.Command("docker", "compose", "exec", "-T", service, "env")
	cmd.Dir = composeDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker compose exec env failed: %w", err)
	}

	env := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		if idx := strings.IndexByte(line, '='); idx > 0 {
			env[line[:idx]] = line[idx+1:]
		}
	}
	return env, nil
}

// ReadWorkerEnvFile reads the worker.env file from the install directory.
func ReadWorkerEnvFile(installDir string) (map[string]string, error) {
	envPath := filepath.Join(installDir, ComposeSubDir, "worker.env")
	cmd := exec.Command("cat", envPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("no worker.env file found")
	}

	env := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx > 0 {
			env[line[:idx]] = line[idx+1:]
		}
	}
	return env, nil
}

// CleanupOldProjectContainers removes containers from the old "temporal" project name.
// This handles the migration from the old naming (temporal-api-1, temporal-worker-1, etc.)
// to the new "reposwarm" project name (reposwarm-api, reposwarm-worker, etc.).
func CleanupOldProjectContainers() {
	// List containers with old "temporal" project label
	out, err := exec.Command("docker", "ps", "-a", "--filter", "label=com.docker.compose.project=temporal", "-q").Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return
	}

	ids := strings.Fields(strings.TrimSpace(string(out)))
	if len(ids) > 0 {
		args := append([]string{"rm", "-f"}, ids...)
		exec.Command("docker", args...).Run()
	}

	// Also remove old volumes
	for _, vol := range []string{"temporal_temporal-data", "temporal_dynamodb-data", "temporal_config-data", "temporal_askbox-output", "temporal_askbox-arch-hub"} {
		exec.Command("docker", "volume", "rm", "-f", vol).Run()
	}
}

// WaitForDockerHealth waits for a Docker container to report healthy status.
// For containers with a healthcheck, it waits for health="healthy".
// For containers without a healthcheck (health=""), it polls twice with a gap
// to verify the container stays in "running" state (not just momentarily).
// Returns nil if healthy within timeout, error otherwise.
func WaitForDockerHealth(installDir, service string, timeoutSec int) error {
	composeDir := filepath.Join(installDir, ComposeSubDir)
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)

	for time.Now().Before(deadline) {
		cmd := exec.Command("docker", "compose", "ps", "--format", "json", service)
		cmd.Dir = composeDir
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			var raw dockerServiceJSON
			if json.Unmarshal(bytes.TrimSpace(out), &raw) == nil {
				health := strings.ToLower(raw.Health)
				state := strings.ToLower(raw.State)
				if health == "healthy" {
					return nil
				}
				// For containers without a healthcheck, verify stability
				if health == "" && state == "running" {
					// Wait and re-check to confirm the container stays running
					time.Sleep(3 * time.Second)
					cmd2 := exec.Command("docker", "compose", "ps", "--format", "json", service)
					cmd2.Dir = composeDir
					out2, err2 := cmd2.Output()
					if err2 == nil && len(out2) > 0 {
						var raw2 dockerServiceJSON
						if json.Unmarshal(bytes.TrimSpace(out2), &raw2) == nil {
							state2 := strings.ToLower(raw2.State)
							if state2 == "running" {
								return nil
							}
							return fmt.Errorf("container %s exited after starting (state: %s)", service, state2)
						}
					}
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for %s to be healthy (%ds)", service, timeoutSec)
}
