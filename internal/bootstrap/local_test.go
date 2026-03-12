package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTemporalComposeLocal_WorkerRestartPolicy verifies the worker service
// has a restart policy to survive race conditions with Temporal startup.
func TestTemporalComposeLocal_WorkerRestartPolicy(t *testing.T) {
	composeYAML := TemporalComposeLocal()

	// Find the worker service block and check for restart policy.
	// The YAML is indented with 2 spaces per level.
	lines := strings.Split(composeYAML, "\n")
	inWorker := false
	foundRestart := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Top-level service key (2-space indent under services)
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(trimmed, ":") {
			inWorker = trimmed == "worker:"
		}
		if inWorker && strings.HasPrefix(trimmed, "restart:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "restart:"))
			if val != "on-failure" {
				t.Errorf("worker restart policy = %q, want %q", val, "on-failure")
			}
			foundRestart = true
		}
	}
	if !foundRestart {
		t.Error("worker service has no restart policy; expected restart: on-failure")
	}
}

// TestTemporalComposeLocal_ServicesExist verifies all expected services are defined.
func TestTemporalComposeLocal_ServicesExist(t *testing.T) {
	composeYAML := TemporalComposeLocal()

	expected := []string{"postgres", "temporal", "temporal-ui", "dynamodb-local", "api", "worker", "ui"}
	for _, svc := range expected {
		if !strings.Contains(composeYAML, "  "+svc+":") {
			t.Errorf("service %q not found in compose YAML", svc)
		}
	}
}

// TestUpdateComposeWorkerMount_AddsMount verifies that UpdateComposeWorkerMount
// adds a bind mount to the worker service in docker-compose.yml.
func TestUpdateComposeWorkerMount_AddsMount(t *testing.T) {
	dir := t.TempDir()
	composeDir := filepath.Join(dir, ComposeSubDir)
	os.MkdirAll(composeDir, 0755)

	// Write the standard compose file
	composePath := filepath.Join(composeDir, "docker-compose.yml")
	os.WriteFile(composePath, []byte(TemporalComposeLocal()), 0644)

	// Add a bind mount
	err := UpdateComposeWorkerMount(dir, "/home/user/docs", "/data/arch-hub")
	if err != nil {
		t.Fatalf("UpdateComposeWorkerMount: %v", err)
	}

	// Read back and verify the mount was added
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("reading compose file: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "/home/user/docs:/data/arch-hub") {
		t.Errorf("mount not found in compose YAML:\n%s", content)
	}
}

// TestUpdateComposeWorkerMount_ReplacesExisting verifies that an existing mount
// with the same container path is replaced, not duplicated.
func TestUpdateComposeWorkerMount_ReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	composeDir := filepath.Join(dir, ComposeSubDir)
	os.MkdirAll(composeDir, 0755)

	composePath := filepath.Join(composeDir, "docker-compose.yml")
	os.WriteFile(composePath, []byte(TemporalComposeLocal()), 0644)

	// Add a mount
	err := UpdateComposeWorkerMount(dir, "/home/user/old", "/data/arch-hub")
	if err != nil {
		t.Fatalf("first UpdateComposeWorkerMount: %v", err)
	}

	// Replace with a different host path
	err = UpdateComposeWorkerMount(dir, "/home/user/new", "/data/arch-hub")
	if err != nil {
		t.Fatalf("second UpdateComposeWorkerMount: %v", err)
	}

	data, _ := os.ReadFile(composePath)
	content := string(data)

	if strings.Contains(content, "/home/user/old") {
		t.Error("old mount should have been replaced")
	}
	if !strings.Contains(content, "/home/user/new:/data/arch-hub") {
		t.Error("new mount not found")
	}
	// Ensure no duplicates
	count := strings.Count(content, "/data/arch-hub")
	if count != 1 {
		t.Errorf("expected 1 occurrence of /data/arch-hub, got %d", count)
	}
}

// TestRemoveComposeWorkerMount_RemovesMount verifies that RemoveComposeWorkerMount
// removes a bind mount with the specified container path.
func TestRemoveComposeWorkerMount_RemovesMount(t *testing.T) {
	dir := t.TempDir()
	composeDir := filepath.Join(dir, ComposeSubDir)
	os.MkdirAll(composeDir, 0755)

	composePath := filepath.Join(composeDir, "docker-compose.yml")
	os.WriteFile(composePath, []byte(TemporalComposeLocal()), 0644)

	// Add a mount first
	err := UpdateComposeWorkerMount(dir, "/home/user/docs", "/data/arch-hub")
	if err != nil {
		t.Fatalf("UpdateComposeWorkerMount: %v", err)
	}

	// Now remove it
	err = RemoveComposeWorkerMount(dir, "/data/arch-hub")
	if err != nil {
		t.Fatalf("RemoveComposeWorkerMount: %v", err)
	}

	data, _ := os.ReadFile(composePath)
	content := string(data)

	if strings.Contains(content, "/data/arch-hub") {
		t.Errorf("mount should have been removed, still found in:\n%s", content)
	}
}

// TestRemoveComposeWorkerMount_PreservesOtherMounts verifies that removing one
// mount does not affect other mounts.
func TestRemoveComposeWorkerMount_PreservesOtherMounts(t *testing.T) {
	dir := t.TempDir()
	composeDir := filepath.Join(dir, ComposeSubDir)
	os.MkdirAll(composeDir, 0755)

	composePath := filepath.Join(composeDir, "docker-compose.yml")
	os.WriteFile(composePath, []byte(TemporalComposeLocal()), 0644)

	// The worker already has a config-data:/data volume from the template
	// Add our custom mount
	err := UpdateComposeWorkerMount(dir, "/home/user/docs", "/data/arch-hub")
	if err != nil {
		t.Fatalf("UpdateComposeWorkerMount: %v", err)
	}

	// Remove only our mount
	err = RemoveComposeWorkerMount(dir, "/data/arch-hub")
	if err != nil {
		t.Fatalf("RemoveComposeWorkerMount: %v", err)
	}

	data, _ := os.ReadFile(composePath)
	content := string(data)

	// config-data:/data should still be there
	if !strings.Contains(content, "config-data:/data") {
		t.Error("config-data:/data volume mount should have been preserved")
	}
}
