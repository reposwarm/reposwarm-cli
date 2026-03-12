package bootstrap

import (
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
