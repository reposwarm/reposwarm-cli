package bootstrap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestWaitForDockerHealth_HealthyContainer verifies that a container with
// healthcheck="healthy" returns nil immediately.
func TestWaitForDockerHealth_HealthyContainer(t *testing.T) {
	dir, cleanup := setupFakeDocker(t, []dockerServiceJSON{
		{Name: "reposwarm-worker", Service: "worker", State: "running", Health: "healthy"},
	})
	defer cleanup()

	err := WaitForDockerHealth(dir, "worker", 5)
	if err != nil {
		t.Errorf("expected nil for healthy container, got: %v", err)
	}
}

// TestWaitForDockerHealth_NoHealthcheckStaysRunning verifies that a container
// without a healthcheck that stays running is accepted after a stability check.
func TestWaitForDockerHealth_NoHealthcheckStaysRunning(t *testing.T) {
	dir, cleanup := setupFakeDocker(t, []dockerServiceJSON{
		{Name: "reposwarm-worker", Service: "worker", State: "running", Health: ""},
	})
	defer cleanup()

	err := WaitForDockerHealth(dir, "worker", 10)
	if err != nil {
		t.Errorf("expected nil for stable running container, got: %v", err)
	}
}

// TestWaitForDockerHealth_NoHealthcheckExited verifies that a container
// without a healthcheck that exits returns an error.
func TestWaitForDockerHealth_NoHealthcheckExited(t *testing.T) {
	// Create a fake docker that first reports "running" then "exited"
	dir, cleanup := setupFakeDockerSequence(t, [][]dockerServiceJSON{
		{{Name: "reposwarm-worker", Service: "worker", State: "running", Health: ""}},
		{{Name: "reposwarm-worker", Service: "worker", State: "exited", Health: ""}},
		{{Name: "reposwarm-worker", Service: "worker", State: "exited", Health: ""}},
	})
	defer cleanup()

	err := WaitForDockerHealth(dir, "worker", 10)
	if err == nil {
		t.Error("expected error for exited container, got nil")
	}
}

// setupFakeDocker creates a temp dir with a docker-compose.yml and a fake
// docker binary that returns the given service JSON (same response every call).
func setupFakeDocker(t *testing.T, services []dockerServiceJSON) (string, func()) {
	t.Helper()
	return setupFakeDockerSequence(t, [][]dockerServiceJSON{services, services, services, services, services})
}

// setupFakeDockerSequence creates a fake docker binary that returns different
// responses on successive calls (for testing state transitions).
func setupFakeDockerSequence(t *testing.T, responses [][]dockerServiceJSON) (string, func()) {
	t.Helper()

	dir := t.TempDir()
	composeDir := filepath.Join(dir, ComposeSubDir)
	os.MkdirAll(composeDir, 0755)
	os.WriteFile(filepath.Join(composeDir, "docker-compose.yml"), []byte("name: reposwarm\n"), 0644)

	// Write each response as a separate JSON file
	for i, resp := range responses {
		var lines string
		for _, svc := range resp {
			data, _ := json.Marshal(svc)
			lines += string(data) + "\n"
		}
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("response_%d.json", i)), []byte(lines), 0644)
	}

	// Create a fake docker script that reads from response files using a counter
	counterFile := filepath.Join(dir, "call_counter")
	os.WriteFile(counterFile, []byte("0"), 0644)

	scriptContent := fmt.Sprintf(`#!/bin/sh
COUNTER_FILE="%s"
DIR="%s"
MAX=%d
COUNT=$(cat "$COUNTER_FILE")
if [ "$COUNT" -ge "$MAX" ]; then
  COUNT=$(( MAX - 1 ))
fi
NEXT=$(( COUNT + 1 ))
echo "$NEXT" > "$COUNTER_FILE"
cat "$DIR/response_${COUNT}.json"
`, counterFile, dir, len(responses))

	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	scriptPath := filepath.Join(binDir, "docker")
	os.WriteFile(scriptPath, []byte(scriptContent), 0755)

	// Prepend our bin to PATH
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", binDir+":"+origPath)

	cleanup := func() {
		os.Setenv("PATH", origPath)
	}

	return dir, cleanup
}
