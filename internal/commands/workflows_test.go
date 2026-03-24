package commands

import (
	"encoding/json"
	"strings"
	"testing"
)

// workflowsListFixture returns a mock /workflows response with mixed statuses.
func workflowsListFixture() map[string]any {
	return map[string]any{
		"/workflows": map[string]any{
			"executions": []map[string]any{
				{"workflowId": "wf-running-1", "status": "Running", "type": "InvestigateSingleRepoWorkflow", "startTime": "2026-01-01T00:00:00Z"},
				{"workflowId": "wf-failed-1", "status": "Failed", "type": "InvestigateSingleRepoWorkflow", "startTime": "2026-01-01T00:00:00Z"},
				{"workflowId": "wf-failed-2", "status": "Failed", "type": "InvestigateSingleRepoWorkflow", "startTime": "2026-01-02T00:00:00Z"},
				{"workflowId": "wf-completed-1", "status": "Completed", "type": "InvestigateSingleRepoWorkflow", "startTime": "2026-01-02T00:00:00Z"},
				{"workflowId": "wf-terminated-1", "status": "Terminated", "type": "InvestigateSingleRepoWorkflow", "startTime": "2026-01-03T00:00:00Z"},
			},
		},
	}
}

// --- RED: these tests should FAIL before the fix is applied ---

// TestWfListStatusFlagExists verifies that `wf list --status` is a recognized flag.
// Before the fix this returns an "unknown flag: --status" error.
func TestWfListStatusFlagExists(t *testing.T) {
	_, cleanup := testServer(t, workflowsListFixture())
	defer cleanup()

	_, err := runCmd(t, "wf", "list", "--status", "failed")
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--status flag does not exist on `wf list`: %v", err)
	}
	// Any other error (e.g. network) is acceptable for this flag-existence test.
}

// TestWfListStatusFilterFailed verifies that --status failed returns only Failed workflows.
func TestWfListStatusFilterFailed(t *testing.T) {
	_, cleanup := testServer(t, workflowsListFixture())
	defer cleanup()

	out, err := runCmd(t, "wf", "list", "--status", "failed", "--json")
	if err != nil {
		t.Fatalf("wf list --status failed: %v", err)
	}

	var executions []map[string]any
	if err := json.Unmarshal([]byte(out), &executions); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}

	if len(executions) != 2 {
		t.Errorf("expected 2 failed workflows, got %d\noutput: %s", len(executions), out)
	}
	for _, wf := range executions {
		if strings.ToLower(wf["status"].(string)) != "failed" {
			t.Errorf("unexpected status %q in filtered results", wf["status"])
		}
	}
}

// TestWfListStatusFilterRunning verifies --status running returns only Running workflows.
func TestWfListStatusFilterRunning(t *testing.T) {
	_, cleanup := testServer(t, workflowsListFixture())
	defer cleanup()

	out, err := runCmd(t, "wf", "list", "--status", "running", "--json")
	if err != nil {
		t.Fatalf("wf list --status running: %v", err)
	}

	var executions []map[string]any
	if err := json.Unmarshal([]byte(out), &executions); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}

	if len(executions) != 1 {
		t.Errorf("expected 1 running workflow, got %d\noutput: %s", len(executions), out)
	}
}

// TestWfListStatusCaseInsensitive verifies that --status FAILED (upper case) works.
func TestWfListStatusCaseInsensitive(t *testing.T) {
	_, cleanup := testServer(t, workflowsListFixture())
	defer cleanup()

	out, err := runCmd(t, "wf", "list", "--status", "FAILED", "--json")
	if err != nil {
		t.Fatalf("wf list --status FAILED: %v", err)
	}

	var executions []map[string]any
	if err := json.Unmarshal([]byte(out), &executions); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}

	if len(executions) != 2 {
		t.Errorf("expected 2 failed workflows with FAILED (uppercase), got %d", len(executions))
	}
}

// TestWfListStatusNoFilter verifies that omitting --status returns all workflows.
func TestWfListStatusNoFilter(t *testing.T) {
	_, cleanup := testServer(t, workflowsListFixture())
	defer cleanup()

	out, err := runCmd(t, "wf", "list", "--json")
	if err != nil {
		t.Fatalf("wf list (no filter): %v", err)
	}

	var executions []map[string]any
	if err := json.Unmarshal([]byte(out), &executions); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}

	if len(executions) != 5 {
		t.Errorf("expected 5 workflows (no filter), got %d", len(executions))
	}
}

// TestWfListStatusInvalidValue verifies that an unknown status value returns an error.
func TestWfListStatusInvalidValue(t *testing.T) {
	_, cleanup := testServer(t, workflowsListFixture())
	defer cleanup()

	_, err := runCmd(t, "wf", "list", "--status", "bogus-status")
	if err == nil {
		t.Fatal("expected error for invalid --status value, got nil")
	}
	if !strings.Contains(err.Error(), "invalid --status") && !strings.Contains(err.Error(), "bogus-status") {
		t.Errorf("error should mention invalid status, got: %v", err)
	}
}

// TestWfListStatusFilterTerminated verifies --status terminated.
func TestWfListStatusFilterTerminated(t *testing.T) {
	_, cleanup := testServer(t, workflowsListFixture())
	defer cleanup()

	out, err := runCmd(t, "wf", "list", "--status", "terminated", "--json")
	if err != nil {
		t.Fatalf("wf list --status terminated: %v", err)
	}

	var executions []map[string]any
	if err := json.Unmarshal([]byte(out), &executions); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}

	if len(executions) != 1 {
		t.Errorf("expected 1 terminated workflow, got %d", len(executions))
	}
}

// TestWfListStatusFilterCompleted verifies --status completed.
func TestWfListStatusFilterCompleted(t *testing.T) {
	_, cleanup := testServer(t, workflowsListFixture())
	defer cleanup()

	out, err := runCmd(t, "wf", "list", "--status", "completed", "--json")
	if err != nil {
		t.Fatalf("wf list --status completed: %v", err)
	}

	var executions []map[string]any
	if err := json.Unmarshal([]byte(out), &executions); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}

	if len(executions) != 1 {
		t.Errorf("expected 1 completed workflow, got %d", len(executions))
	}
}

// TestWfListStatusTableOutput verifies non-JSON output still works with --status.
func TestWfListStatusTableOutput(t *testing.T) {
	_, cleanup := testServer(t, workflowsListFixture())
	defer cleanup()

	out, err := runCmd(t, "wf", "list", "--status", "failed")
	if err != nil {
		t.Fatalf("wf list --status failed (table): %v", err)
	}

	if !strings.Contains(out, "wf-failed-1") || !strings.Contains(out, "wf-failed-2") {
		t.Errorf("table output should contain failed workflow IDs\noutput: %s", out)
	}
	if strings.Contains(out, "wf-running-1") {
		t.Errorf("table output should NOT contain running workflow when filtered to failed\noutput: %s", out)
	}
}

// --- Ghost flag: `results read --all` ---
// workflows_watch_repo.go:359 suggests `reposwarm results read <repo> --all`
// but `results read` has no --all flag.
// Fix: add --all to results read (explicit flag; redundant but documents intent
// and prevents the hint from pointing to a broken command).

// TestResultsReadAllFlagExists verifies that `results read --all` is accepted.
// RED before fix: returns "unknown flag: --all".
func TestResultsReadAllFlagExists(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /wiki/is-odd": map[string]any{
			"repo":     "is-odd",
			"sections": []map[string]any{{"id": "hl_overview", "label": "Overview", "createdAt": "2026-01-01"}},
			"hasDocs":  true,
		},
		"GET /wiki/is-odd/hl_overview": map[string]any{
			"repo":    "is-odd",
			"section": "hl_overview",
			"content": "# Overview\nTest content.",
			"createdAt": "2026-01-01",
		},
	})
	defer cleanup()

	_, err := runCmd(t, "results", "read", "is-odd", "--all")
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("`results read --all` flag does not exist (ghost flag): %v", err)
	}
}

// TestResultsReadAllEquivalentToNoSection verifies --all produces the same output as
// omitting the section argument (both read all sections).
func TestResultsReadAllEquivalentToNoSection(t *testing.T) {
	routes := map[string]any{
		"GET /wiki/is-odd": map[string]any{
			"repo":     "is-odd",
			"sections": []map[string]any{{"id": "hl_overview", "label": "Overview", "createdAt": "2026-01-01"}},
			"hasDocs":  true,
		},
		"GET /wiki/is-odd/hl_overview": map[string]any{
			"repo":    "is-odd",
			"section": "hl_overview",
			"content": "# Overview\nTest content.",
			"createdAt": "2026-01-01",
		},
	}

	_, cleanup1 := testServer(t, routes)
	defer cleanup1()

	outAll, err := runCmd(t, "results", "read", "is-odd", "--all", "--raw")
	if err != nil {
		t.Fatalf("results read --all: %v", err)
	}

	_, cleanup2 := testServer(t, routes)
	defer cleanup2()

	outNoFlag, err := runCmd(t, "results", "read", "is-odd", "--raw")
	if err != nil {
		t.Fatalf("results read (no --all): %v", err)
	}

	if outAll != outNoFlag {
		t.Errorf("--all should produce same output as no flag\nwith --all: %q\nwithout:   %q", outAll, outNoFlag)
	}
}
