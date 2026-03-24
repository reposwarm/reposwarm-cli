package commands

import (
	"testing"

	"github.com/reposwarm/reposwarm-cli/internal/api"
	"github.com/reposwarm/reposwarm-cli/internal/bootstrap"
)

// TestApplyDockerOverlay_RunningWorker verifies that a worker reported as
// "stopped" by the API is overridden to "healthy" when the Docker service is running.
func TestApplyDockerOverlay_RunningWorker(t *testing.T) {
	workers := []api.WorkerInfo{
		{Name: "worker-1", Status: "stopped"},
	}
	dockerServices := []bootstrap.DockerService{
		{Name: "reposwarm-worker-1", Service: "worker", State: "running", Health: ""},
	}
	result := applyDockerOverlay(workers, dockerServices, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(result))
	}
	if result[0].Status != "healthy" {
		t.Errorf("expected status 'healthy', got %q", result[0].Status)
	}
	if result[0].Host != "reposwarm-worker-1" {
		t.Errorf("expected host 'reposwarm-worker-1', got %q", result[0].Host)
	}
}

// TestApplyDockerOverlay_DegradedWorker verifies that a running but unhealthy
// Docker container results in "degraded" status.
func TestApplyDockerOverlay_DegradedWorker(t *testing.T) {
	workers := []api.WorkerInfo{
		{Name: "worker-1", Status: "stopped"},
	}
	dockerServices := []bootstrap.DockerService{
		{Name: "reposwarm-worker-1", Service: "worker", State: "running", Health: "unhealthy"},
	}
	result := applyDockerOverlay(workers, dockerServices, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(result))
	}
	if result[0].Status != "degraded" {
		t.Errorf("expected status 'degraded', got %q", result[0].Status)
	}
}

// TestApplyDockerOverlay_StoppedContainer verifies that a stopped Docker
// container does not override the API status.
func TestApplyDockerOverlay_StoppedContainer(t *testing.T) {
	workers := []api.WorkerInfo{
		{Name: "worker-1", Status: "stopped"},
	}
	dockerServices := []bootstrap.DockerService{
		{Name: "reposwarm-worker-1", Service: "worker", State: "exited", Health: ""},
	}
	result := applyDockerOverlay(workers, dockerServices, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(result))
	}
	// Status should not be overridden if container is not running
	if result[0].Status != "stopped" {
		t.Errorf("expected status 'stopped', got %q", result[0].Status)
	}
}

// TestApplyDockerOverlay_NoDockerServices verifies that an empty docker
// services list results in no status change.
func TestApplyDockerOverlay_NoDockerServices(t *testing.T) {
	workers := []api.WorkerInfo{
		{Name: "worker-1", Status: "stopped"},
	}
	result := applyDockerOverlay(workers, nil, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(result))
	}
	if result[0].Status != "stopped" {
		t.Errorf("expected status 'stopped' (unchanged), got %q", result[0].Status)
	}
}

// TestApplyDockerOverlay_EnvErrorsFilteredByWorkerEnv verifies that env
// errors for variables present in worker.env are removed and EnvStatus
// is set to "OK" when no errors remain.
func TestApplyDockerOverlay_EnvErrorsFilteredByWorkerEnv(t *testing.T) {
	workers := []api.WorkerInfo{
		{
			Name:      "worker-1",
			Status:    "stopped",
			EnvErrors: []string{"ANTHROPIC_API_KEY", "MISSING_VAR"},
			EnvStatus: "error",
		},
	}
	dockerServices := []bootstrap.DockerService{
		{Name: "reposwarm-worker-1", Service: "worker", State: "running", Health: ""},
	}
	workerEnv := map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-xxx",
	}
	result := applyDockerOverlay(workers, dockerServices, workerEnv)
	if len(result) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(result))
	}
	// ANTHROPIC_API_KEY should be removed because it's in workerEnv
	for _, e := range result[0].EnvErrors {
		if e == "ANTHROPIC_API_KEY" {
			t.Error("ANTHROPIC_API_KEY should have been filtered (it is set in worker.env)")
		}
	}
	// MISSING_VAR should remain since it's not in workerEnv
	found := false
	for _, e := range result[0].EnvErrors {
		if e == "MISSING_VAR" {
			found = true
			break
		}
	}
	if !found {
		t.Error("MISSING_VAR should remain in env errors (not in worker.env)")
	}
}

// TestApplyDockerOverlay_AllEnvErrorsClearedSetsOK verifies that when all
// env errors are resolved by worker.env, EnvStatus is set to "OK".
func TestApplyDockerOverlay_AllEnvErrorsClearedSetsOK(t *testing.T) {
	workers := []api.WorkerInfo{
		{
			Name:      "worker-1",
			Status:    "stopped",
			EnvErrors: []string{"ANTHROPIC_API_KEY"},
			EnvStatus: "error",
		},
	}
	dockerServices := []bootstrap.DockerService{
		{Name: "reposwarm-worker-1", Service: "worker", State: "running", Health: ""},
	}
	workerEnv := map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-xxx",
	}
	result := applyDockerOverlay(workers, dockerServices, workerEnv)
	if len(result) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(result))
	}
	if len(result[0].EnvErrors) != 0 {
		t.Errorf("expected no env errors, got %v", result[0].EnvErrors)
	}
	if result[0].EnvStatus != "OK" {
		t.Errorf("expected EnvStatus 'OK', got %q", result[0].EnvStatus)
	}
}

// TestGatherWorkerInfoNonDocker verifies that on a non-Docker install the
// API response is returned unchanged (no overlay applied).
// We test this indirectly via applyDockerOverlay with an empty docker services list.
func TestGatherWorkerInfoNonDocker(t *testing.T) {
	// Non-Docker: we pass nil services (same as what happens when not a Docker install)
	workers := []api.WorkerInfo{
		{Name: "worker-1", Status: "stopped"},
	}
	result := applyDockerOverlay(workers, nil, nil)
	if result[0].Status != "stopped" {
		t.Errorf("non-Docker: expected status 'stopped' (unchanged), got %q", result[0].Status)
	}
}
