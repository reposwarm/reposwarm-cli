package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// testServer creates a mock API server with route handlers.
func testServer(t *testing.T, routes map[string]any) (*httptest.Server, func()) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check auth
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}

		// Match route: "METHOD /path"
		key := r.Method + " " + r.URL.Path
		if handler, ok := routes[key]; ok {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"data": handler})
			return
		}

		// Try path-only match
		if handler, ok := routes[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"data": handler})
			return
		}

		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))

	// Set up config to use test server
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	cleanup := func() {
		os.Setenv("HOME", origHome)
		server.Close()
	}

	// Write config
	cfgDir := dir + "/.reposwarm"
	os.MkdirAll(cfgDir, 0700)
	cfg := map[string]any{
		"apiUrl":   server.URL,
		"apiToken": "test-token",
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(cfgDir+"/config.json", data, 0600)

	return server, cleanup
}

// runCmd executes a command and returns stdout.
func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd("test")

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := root.Execute()

	w.Close()
	os.Stdout = old

	var captured bytes.Buffer
	captured.ReadFrom(r)

	return captured.String(), err
}

func TestStatusCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{
			"status":  "healthy",
			"version": "1.0.0",
			"temporal": map[string]any{
				"connected": true,
				"namespace": "default",
				"taskQueue": "repo-swarm-queue",
			},
			"dynamodb": map[string]any{"connected": true},
			"worker":   map[string]any{"connected": true, "count": 2},
		},
	})
	defer cleanup()

	out, err := runCmd(t, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "healthy") {
		t.Errorf("output should contain 'healthy', got: %s", out)
	}
}

func TestStatusCmdJSON(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{
			"status":   "healthy",
			"version":  "1.0.0",
			"temporal": map[string]any{"connected": true},
			"dynamodb": map[string]any{"connected": true},
			"worker":   map[string]any{"connected": false},
		},
	})
	defer cleanup()

	out, err := runCmd(t, "status", "--json")
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if result["connected"] != true {
		t.Error("expected connected=true")
	}
}

func TestReposListCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /repos": []map[string]any{
			{"name": "repo1", "source": "CodeCommit", "enabled": true, "hasDocs": true},
			{"name": "repo2", "source": "GitHub", "enabled": false, "hasDocs": false},
		},
	})
	defer cleanup()

	out, err := runCmd(t, "repos", "list")
	if err != nil {
		t.Fatalf("repos list: %v", err)
	}
	if !strings.Contains(out, "repo1") || !strings.Contains(out, "repo2") {
		t.Errorf("output should contain repos, got: %s", out)
	}
}

func TestReposListCmdJSON(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /repos": []map[string]any{
			{"name": "repo1", "source": "CodeCommit", "enabled": true},
			{"name": "repo2", "source": "GitHub", "enabled": false},
		},
	})
	defer cleanup()

	out, err := runCmd(t, "repos", "list", "--json")
	if err != nil {
		t.Fatalf("repos list --json: %v", err)
	}

	var repos []map[string]any
	if err := json.Unmarshal([]byte(out), &repos); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("got %d repos, want 2", len(repos))
	}
}

func TestReposListFilterSource(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /repos": []map[string]any{
			{"name": "repo1", "source": "CodeCommit", "enabled": true},
			{"name": "repo2", "source": "GitHub", "enabled": true},
		},
	})
	defer cleanup()

	out, err := runCmd(t, "repos", "list", "--source", "GitHub", "--json")
	if err != nil {
		t.Fatalf("repos list --source GitHub: %v", err)
	}

	var repos []map[string]any
	json.Unmarshal([]byte(out), &repos)
	if len(repos) != 1 {
		t.Errorf("got %d repos after filter, want 1", len(repos))
	}
}

func TestDiscoverCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"POST /repos/discover": map[string]any{
			"success": true, "discovered": 36, "added": 5, "skipped": 31, "total": 36,
		},
	})
	defer cleanup()

	out, err := runCmd(t, "repos", "discover")
	if err != nil {
		t.Fatalf("repos discover: %v", err)
	}
	if !strings.Contains(out, "36") {
		t.Errorf("output should mention 36 repos: %s", out)
	}
}

func TestDiscoverCmdWithSource(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"POST /repos/discover": map[string]any{
			"success": true, "discovered": 10, "added": 2, "skipped": 8, "total": 10,
		},
	})
	defer cleanup()

	out, err := runCmd(t, "repos", "discover", "--source", "github")
	if err != nil {
		t.Fatalf("repos discover --source github: %v", err)
	}
	if !strings.Contains(out, "10") {
		t.Errorf("output should mention 10 repos: %s", out)
	}
}

func TestDiscoverCmdWithOrg(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"POST /repos/discover": map[string]any{
			"success": true, "discovered": 5, "added": 5, "skipped": 0, "total": 5,
		},
	})
	defer cleanup()

	out, err := runCmd(t, "repos", "discover", "--source", "github", "--org", "myorg")
	if err != nil {
		t.Fatalf("repos discover --org: %v", err)
	}
	if !strings.Contains(out, "5") {
		t.Errorf("output should mention 5 repos: %s", out)
	}
}

func TestDiscoverCmdJSON(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"POST /repos/discover": map[string]any{
			"success": true, "discovered": 10, "added": 3, "skipped": 7, "total": 10,
		},
	})
	defer cleanup()

	out, err := runCmd(t, "repos", "discover", "--json")
	if err != nil {
		t.Fatalf("repos discover --json: %v", err)
	}

	var result map[string]any
	json.Unmarshal([]byte(out), &result)
	if result["discovered"] != float64(10) {
		t.Errorf("discovered = %v, want 10", result["discovered"])
	}
}

func TestWorkflowsListCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/workflows": map[string]any{
			"executions": []map[string]any{
				{"workflowId": "wf-1", "status": "Running", "type": "Investigate", "startTime": "2026-01-01"},
				{"workflowId": "wf-2", "status": "Completed", "type": "Investigate", "startTime": "2026-01-01"},
			},
		},
	})
	defer cleanup()

	out, err := runCmd(t, "workflows", "list")
	if err != nil {
		t.Fatalf("workflows list: %v", err)
	}
	if !strings.Contains(out, "wf-1") {
		t.Errorf("output should contain wf-1: %s", out)
	}
}

func TestWorkflowsStatusCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /workflows/wf-1": map[string]any{
			"workflowId": "wf-1", "runId": "run-1", "status": "Running",
			"type": "Investigate", "startTime": "2026-01-01",
		},
	})
	defer cleanup()

	out, err := runCmd(t, "workflows", "status", "wf-1", "--json")
	if err != nil {
		t.Fatalf("workflows status: %v", err)
	}

	var wf map[string]any
	json.Unmarshal([]byte(out), &wf)
	if wf["status"] != "Running" {
		t.Errorf("status = %v, want Running", wf["status"])
	}
}

func TestResultsListCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /wiki": map[string]any{
			"repos": []map[string]any{
				{"name": "is-odd", "sectionCount": 19, "lastUpdated": "2026-01-01"},
			},
		},
	})
	defer cleanup()

	out, err := runCmd(t, "results", "list")
	if err != nil {
		t.Fatalf("results list: %v", err)
	}
	if !strings.Contains(out, "is-odd") {
		t.Errorf("output should contain is-odd: %s", out)
	}
}

func TestResultsShowCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /wiki/is-odd": map[string]any{
			"repo":     "is-odd",
			"sections": []map[string]any{{"id": "hl_overview", "label": "Overview", "createdAt": "2026-01-01"}},
			"hasDocs":  true,
		},
	})
	defer cleanup()

	out, err := runCmd(t, "results", "show", "is-odd")
	if err != nil {
		t.Fatalf("results show: %v", err)
	}
	if !strings.Contains(out, "hl_overview") {
		t.Errorf("output should contain hl_overview: %s", out)
	}
}

func TestResultsReadSectionCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /wiki/is-odd/hl_overview": map[string]any{
			"repo": "is-odd", "section": "hl_overview",
			"content": "# Overview\nThis is is-odd.", "createdAt": "2026-01-01",
		},
	})
	defer cleanup()

	out, err := runCmd(t, "results", "read", "is-odd", "hl_overview", "--raw")
	if err != nil {
		t.Fatalf("results read: %v", err)
	}
	if !strings.Contains(out, "# Overview") {
		t.Errorf("output should contain markdown: %s", out)
	}
}

func TestResultsMetaCmdJSON(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /wiki/is-odd/hl_overview": map[string]any{
			"repo": "is-odd", "section": "hl_overview",
			"content": "test", "createdAt": "2026-01-01",
			"timestamp": 1234567890, "referenceKey": "ref-123",
		},
	})
	defer cleanup()

	out, err := runCmd(t, "results", "meta", "is-odd", "hl_overview", "--json")
	if err != nil {
		t.Fatalf("results meta: %v", err)
	}

	var meta map[string]any
	json.Unmarshal([]byte(out), &meta)
	if meta["section"] != "hl_overview" {
		t.Errorf("section = %v, want hl_overview", meta["section"])
	}
}

func TestPromptsListCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /prompts": []map[string]any{
			{"name": "overview", "type": "base", "enabled": true, "order": 1, "version": 2},
			{"name": "security", "type": "base", "enabled": false, "order": 5, "version": 1},
		},
	})
	defer cleanup()

	out, err := runCmd(t, "prompts", "list")
	if err != nil {
		t.Fatalf("prompts list: %v", err)
	}
	if !strings.Contains(out, "overview") {
		t.Errorf("output should contain overview: %s", out)
	}
}

func TestPromptsListEnabledFilter(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /prompts": []map[string]any{
			{"name": "overview", "type": "base", "enabled": true, "order": 1, "version": 2},
			{"name": "security", "type": "base", "enabled": false, "order": 5, "version": 1},
		},
	})
	defer cleanup()

	out, err := runCmd(t, "prompts", "list", "--enabled", "--json")
	if err != nil {
		t.Fatalf("prompts list --enabled: %v", err)
	}

	var prompts []map[string]any
	json.Unmarshal([]byte(out), &prompts)
	if len(prompts) != 1 {
		t.Errorf("got %d prompts, want 1 (only enabled)", len(prompts))
	}
}

func TestPromptsShowCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /prompts/overview": map[string]any{
			"name": "overview", "type": "base", "description": "High-level overview",
			"template": "# {{repo}}\nAnalyze...", "enabled": true, "order": 1, "version": 3,
		},
	})
	defer cleanup()

	out, err := runCmd(t, "prompts", "show", "overview", "--json")
	if err != nil {
		t.Fatalf("prompts show: %v", err)
	}

	var prompt map[string]any
	json.Unmarshal([]byte(out), &prompt)
	if prompt["name"] != "overview" {
		t.Errorf("name = %v, want overview", prompt["name"])
	}
}

func TestPromptsVersionsCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /prompts/overview/versions": []map[string]any{
			{"version": 1, "createdAt": "2026-01-01", "author": "system"},
			{"version": 2, "createdAt": "2026-01-15", "author": "cli"},
		},
	})
	defer cleanup()

	out, err := runCmd(t, "prompts", "versions", "overview")
	if err != nil {
		t.Fatalf("prompts versions: %v", err)
	}
	if !strings.Contains(out, "v1") || !strings.Contains(out, "v2") {
		t.Errorf("output should contain versions: %s", out)
	}
}

func TestPromptsTypesCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /prompts/types": []map[string]any{
			{"name": "base", "count": 15},
			{"name": "detection", "count": 3},
		},
	})
	defer cleanup()

	out, err := runCmd(t, "prompts", "types", "--json")
	if err != nil {
		t.Fatalf("prompts types: %v", err)
	}

	var types []map[string]any
	json.Unmarshal([]byte(out), &types)
	if len(types) != 2 {
		t.Errorf("got %d types, want 2", len(types))
	}
}

func TestServerConfigShowCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /config": map[string]any{
			"defaultModel": "claude-sonnet", "chunkSize": 10,
			"sleepDuration": 2000, "parallelLimit": 3,
			"tokenLimit": 200000, "scheduleExpression": "rate(6 hours)",
		},
	})
	defer cleanup()

	out, err := runCmd(t, "config", "server")
	if err != nil {
		t.Fatalf("config server: %v", err)
	}
	if !strings.Contains(out, "claude-sonnet") {
		t.Errorf("output should contain model: %s", out)
	}
}

func TestConfigShowCmd(t *testing.T) {
	_, cleanup := testServer(t, nil)
	defer cleanup()

	out, err := runCmd(t, "config", "show")
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	if !strings.Contains(out, "apiUrl") {
		t.Errorf("output should show config: %s", out)
	}
}

func TestConfigShowCmdJSON(t *testing.T) {
	_, cleanup := testServer(t, nil)
	defer cleanup()

	out, err := runCmd(t, "config", "show", "--json")
	if err != nil {
		t.Fatalf("config show --json: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["apiUrl"] == nil {
		t.Error("expected apiUrl in JSON output")
	}
}

func TestVersionFlag(t *testing.T) {
	root := NewRootCmd("1.2.3")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--version"})
	root.Execute()

	if !strings.Contains(buf.String(), "1.2.3") {
		t.Errorf("version output = %s, want 1.2.3", buf.String())
	}
}

func TestNoTokenError(t *testing.T) {
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	// No config file = no token
	_, err := runCmd(t, "repos", "list")
	if err == nil {
		t.Error("expected error when no token configured")
	}
}

func TestInvestigateSingleCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"POST /investigate/single": map[string]any{
			"workflowId": "investigate-single-test",
			"success":    true,
		},
	})
	defer cleanup()

	out, err := runCmd(t, "investigate", "test-repo", "--json", "--force")
	if err != nil {
		t.Fatalf("investigate: %v", err)
	}

	var result map[string]any
	json.Unmarshal([]byte(out), &result)
	if result["workflowId"] != "investigate-single-test" {
		t.Errorf("workflowId = %v", result["workflowId"])
	}
}

func TestInvestigateNoArgs(t *testing.T) {
	_, cleanup := testServer(t, nil)
	defer cleanup()

	_, err := runCmd(t, "investigate")
	if err == nil {
		t.Error("expected error when no repo specified")
	}
}

func TestDiffCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /wiki/repo1": map[string]any{
			"repo":     "repo1",
			"sections": []map[string]any{{"id": "overview"}, {"id": "apis"}},
			"hasDocs":  true,
		},
		"GET /wiki/repo2": map[string]any{
			"repo":     "repo2",
			"sections": []map[string]any{{"id": "overview"}, {"id": "security"}},
			"hasDocs":  true,
		},
	})
	defer cleanup()

	out, err := runCmd(t, "results", "diff", "repo1", "repo2", "--json")
	if err != nil {
		t.Fatalf("results diff: %v", err)
	}

	var result map[string]any
	json.Unmarshal([]byte(out), &result)
	if result["repo1"] != "repo1" {
		t.Errorf("repo1 = %v", result["repo1"])
	}
}

func TestDoctorCmdRegistered(t *testing.T) {
	root := NewRootCmd("test")
	for _, c := range root.Commands() {
		if c.Name() == "doctor" {
			if !strings.Contains(c.Short, "Diagnose") {
				t.Errorf("doctor short = %s", c.Short)
			}
			return
		}
	}
	t.Error("doctor command not registered")
}

func TestReposShowCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /repos/is-odd": map[string]any{
			"name": "is-odd", "source": "GitHub", "url": "https://github.com/jonschlinkert/is-odd",
			"enabled": true, "hasDocs": true, "status": "active",
		},
	})
	defer cleanup()

	out, err := runCmd(t, "repos", "show", "is-odd", "--json")
	if err != nil {
		t.Fatalf("repos show: %v", err)
	}

	var repo map[string]any
	json.Unmarshal([]byte(out), &repo)
	if repo["name"] != "is-odd" {
		t.Errorf("name = %v, want is-odd", repo["name"])
	}
}

func TestUpgradeCmdJSON(t *testing.T) {
	root := NewRootCmd("1.0.0")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"upgrade", "--json"})

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	root.Execute()
	w.Close()
	os.Stdout = old

	var captured bytes.Buffer
	captured.ReadFrom(r)
	out := captured.String()

	if out != "" {
		var result map[string]any
		if err := json.Unmarshal([]byte(out), &result); err == nil {
			if result["current"] != "1.0.0" {
				t.Errorf("current = %v, want 1.0.0", result["current"])
			}
		}
	}
}

func TestReportCmd(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"GET /wiki": map[string]any{
			"repos": []map[string]any{
				{"name": "repo1", "sectionCount": 2, "lastUpdated": "2026-01-01"},
			},
		},
		"GET /wiki/repo1": map[string]any{
			"repo":     "repo1",
			"sections": []map[string]any{{"id": "overview", "label": "Overview", "createdAt": "2026-01-01"}},
			"hasDocs":  true,
		},
		"GET /wiki/repo1/overview": map[string]any{
			"repo": "repo1", "section": "overview",
			"content": "# Overview\nTest content", "createdAt": "2026-01-01",
		},
	})
	defer cleanup()

	out, err := runCmd(t, "results", "report", "repo1", "--json")
	if err != nil {
		t.Fatalf("results report: %v", err)
	}

	var reports []map[string]any
	if err := json.Unmarshal([]byte(out), &reports); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(reports) != 1 {
		t.Errorf("got %d reports, want 1", len(reports))
	}
	if reports[0]["name"] != "repo1" {
		t.Errorf("name = %v, want repo1", reports[0]["name"])
	}
}

func TestNewCmdGuideOnly(t *testing.T) {
	dir := t.TempDir()
	out, err := runCmd(t, "new", "--guide-only", "--dir", dir)
	if err != nil {
		t.Fatalf("new --guide-only: %v", err)
	}
	_ = out

	// Check files were created
	if _, err := os.Stat(dir + "/INSTALL.md"); err != nil {
		t.Error("INSTALL.md not created")
	}
	if _, err := os.Stat(dir + "/REPOSWARM_INSTALL.md"); err != nil {
		t.Error("REPOSWARM_INSTALL.md not created")
	}
}

func TestNewCmdJSON(t *testing.T) {
	dir := t.TempDir()
	out, err := runCmd(t, "new", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("new --json: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if result["installDir"] != dir {
		t.Errorf("installDir = %v, want %s", result["installDir"], dir)
	}
	env, ok := result["environment"].(map[string]any)
	if !ok {
		t.Fatal("expected environment object")
	}
	if env["os"] == nil {
		t.Error("expected os in environment")
	}
}

// --- Top-level `discover` alias (added v1.3.178) ---
// `reposwarm discover` is a top-level alias for `repos discover`.
// These tests verify it's registered and behaves identically to `repos discover`.

func TestTopLevelDiscoverExists(t *testing.T) {
	root := NewRootCmd("test")
	for _, c := range root.Commands() {
		if c.Name() == "discover" {
			return // found
		}
	}
	t.Error("top-level `discover` command not registered on root")
}

func TestTopLevelDiscoverRuns(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"POST /repos/discover": map[string]any{
			"success": true, "discovered": 7, "added": 2, "skipped": 5, "total": 7,
		},
	})
	defer cleanup()

	out, err := runCmd(t, "discover", "--json")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if result["discovered"] != float64(7) {
		t.Errorf("discovered = %v, want 7", result["discovered"])
	}
}

func TestTopLevelDiscoverWithSource(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"POST /repos/discover": map[string]any{
			"success": true, "discovered": 3, "added": 3, "skipped": 0, "total": 3,
		},
	})
	defer cleanup()

	out, err := runCmd(t, "discover", "--source", "github", "--json")
	if err != nil {
		t.Fatalf("discover --source github: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if result["discovered"] != float64(3) {
		t.Errorf("discovered = %v, want 3", result["discovered"])
	}
}

func TestTopLevelDiscoverWithOrg(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"POST /repos/discover": map[string]any{
			"success": true, "discovered": 5, "added": 5, "skipped": 0, "total": 5,
		},
	})
	defer cleanup()

	out, err := runCmd(t, "discover", "--source", "github", "--org", "myorg", "--json")
	if err != nil {
		t.Fatalf("discover --source github --org myorg: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if result["discovered"] != float64(5) {
		t.Errorf("discovered = %v, want 5", result["discovered"])
	}
}
