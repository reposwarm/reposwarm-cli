package commands

import (
	"os"
	"strings"
	"testing"
)

func TestConfigGitSetGitHub(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
	})
	defer cleanup()

	// Use --for-agent to skip interactive prompts (applyGitProvider returns early)
	out, err := runCmd(t, "config", "git", "set", "github", "--for-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "Git provider set to GitHub") {
		t.Errorf("expected success message, got: %s", out)
	}
	if !strings.Contains(out, "GITHUB_TOKEN") {
		t.Errorf("expected GITHUB_TOKEN in output, got: %s", out)
	}
}

func TestConfigGitSetCodeCommit(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
	})
	defer cleanup()

	out, err := runCmd(t, "config", "git", "set", "codecommit", "--for-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "Git provider set to AWS CodeCommit") {
		t.Errorf("expected CodeCommit success message, got: %s", out)
	}
	if !strings.Contains(out, "AWS_REGION") {
		t.Errorf("expected AWS_REGION in output, got: %s", out)
	}
}

func TestConfigGitSetGitLab(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
	})
	defer cleanup()

	out, err := runCmd(t, "config", "git", "set", "gitlab", "--for-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "Git provider set to GitLab") {
		t.Errorf("expected GitLab success message, got: %s", out)
	}
	if !strings.Contains(out, "GITLAB_TOKEN") {
		t.Errorf("expected GITLAB_TOKEN in output, got: %s", out)
	}
}

func TestConfigGitSetAzure(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
	})
	defer cleanup()

	out, err := runCmd(t, "config", "git", "set", "azure", "--for-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "Git provider set to Azure DevOps") {
		t.Errorf("expected Azure success message, got: %s", out)
	}
	if !strings.Contains(out, "AZURE_DEVOPS_PAT") {
		t.Errorf("expected AZURE_DEVOPS_PAT in output, got: %s", out)
	}
	if !strings.Contains(out, "AZURE_DEVOPS_ORG") {
		t.Errorf("expected AZURE_DEVOPS_ORG in output, got: %s", out)
	}
}

func TestConfigGitSetBitbucket(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
	})
	defer cleanup()

	out, err := runCmd(t, "config", "git", "set", "bitbucket", "--for-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "Git provider set to Bitbucket") {
		t.Errorf("expected Bitbucket success message, got: %s", out)
	}
	if !strings.Contains(out, "BITBUCKET_USERNAME") {
		t.Errorf("expected BITBUCKET_USERNAME in output, got: %s", out)
	}
	if !strings.Contains(out, "BITBUCKET_APP_PASSWORD") {
		t.Errorf("expected BITBUCKET_APP_PASSWORD in output, got: %s", out)
	}
}

func TestConfigGitSetInvalidProvider(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
	})
	defer cleanup()

	_, err := runCmd(t, "config", "git", "set", "svn", "--for-agent")
	if err == nil {
		t.Fatal("expected error for invalid provider")
	}
	if !strings.Contains(err.Error(), "unknown git provider") {
		t.Errorf("expected 'unknown git provider' error, got: %v", err)
	}
}

func TestConfigGitShowEmpty(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
	})
	defer cleanup()

	out, err := runCmd(t, "config", "git", "show", "--for-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "not configured") {
		t.Errorf("expected 'not configured' for empty git provider, got: %s", out)
	}
}

func TestConfigGitShowAfterSet(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
	})
	defer cleanup()

	// Set first
	_, err := runCmd(t, "config", "git", "set", "github", "--for-agent")
	if err != nil {
		t.Fatalf("set failed: %v", err)
	}

	// Show
	out, err := runCmd(t, "config", "git", "show", "--for-agent")
	if err != nil {
		t.Fatalf("show failed: %v", err)
	}

	if !strings.Contains(out, "git_provider: github") {
		t.Errorf("expected 'git_provider: github', got: %s", out)
	}
	if !strings.Contains(out, "GITHUB_TOKEN") {
		t.Errorf("expected GITHUB_TOKEN in show output, got: %s", out)
	}
}

func TestConfigGitShowJSON(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
	})
	defer cleanup()

	// Set first
	_, err := runCmd(t, "config", "git", "set", "gitlab", "--for-agent")
	if err != nil {
		t.Fatalf("set failed: %v", err)
	}

	out, err := runCmd(t, "config", "git", "show", "--json")
	if err != nil {
		t.Fatalf("show --json failed: %v", err)
	}

	if !strings.Contains(out, `"gitProvider":"gitlab"`) && !strings.Contains(out, `"gitProvider": "gitlab"`) {
		t.Errorf("expected gitProvider=gitlab in JSON, got: %s", out)
	}
	if !strings.Contains(out, "GITLAB_TOKEN") {
		t.Errorf("expected GITLAB_TOKEN in JSON output, got: %s", out)
	}
}

func TestConfigGitSetJSON(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
	})
	defer cleanup()

	out, err := runCmd(t, "config", "git", "set", "github", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, `"gitProvider":"github"`) && !strings.Contains(out, `"gitProvider": "github"`) {
		t.Errorf("expected gitProvider in JSON, got: %s", out)
	}
	if !strings.Contains(out, `"saved":true`) && !strings.Contains(out, `"saved": true`) {
		t.Errorf("expected saved:true in JSON, got: %s", out)
	}
}

func TestConfigGitSetSavesToConfig(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
	})
	defer cleanup()

	// Set to azure
	_, err := runCmd(t, "config", "git", "set", "azure", "--for-agent")
	if err != nil {
		t.Fatalf("set failed: %v", err)
	}

	// Read the config file directly
	home := os.Getenv("HOME")
	data, err := os.ReadFile(home + "/.reposwarm/config.json")
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	if !strings.Contains(string(data), `"gitProvider"`) {
		t.Errorf("config file missing gitProvider field: %s", string(data))
	}
	if !strings.Contains(string(data), `azure`) {
		t.Errorf("config file missing 'azure' value: %s", string(data))
	}
}

func TestConfigGitSetupInteractiveWithStdin(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
		"GET /workers/worker-1/env": map[string]any{
			"entries": []map[string]any{
				{"key": "AWS_REGION", "value": "us-east-1", "set": true},
			},
		},
	})
	defer cleanup()

	// Pipe "4\n\n" to select github (sorted: azure=1, bitbucket=2, codecommit=3, github=4, gitlab=5)
	// then press enter to keep AWS_REGION (empty = keep current)
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("4\n")  // select github
		w.WriteString("ghp_test123\n") // enter token
		w.Close()
	}()

	out, err := runCmd(t, "config", "git", "setup")
	if err != nil {
		t.Fatalf("setup failed: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "Git Provider Setup") {
		t.Errorf("expected setup header, got: %s", out)
	}
	if !strings.Contains(out, "GitHub") {
		t.Errorf("expected GitHub in output, got: %s", out)
	}
}

func TestConfigGitSetupShowsAllProviders(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
		"GET /workers/worker-1/env": map[string]any{
			"entries": []map[string]any{},
		},
	})
	defer cleanup()

	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("4\n") // select github
		w.WriteString("\n")  // skip GITHUB_TOKEN
		w.Close()
	}()

	out, err := runCmd(t, "config", "git", "setup")
	if err != nil {
		t.Fatalf("setup failed: %v\noutput: %s", err, out)
	}

	// All 5 providers should be listed
	for _, name := range []string{"GitHub", "CodeCommit", "GitLab", "Azure DevOps", "Bitbucket"} {
		if !strings.Contains(out, name) {
			t.Errorf("expected %s in setup output, got: %s", name, out)
		}
	}
}

func TestConfigGitSetupCodeCommitPromptsRegion(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
		"GET /workers/worker-1/env": map[string]any{
			"entries": []map[string]any{
				{"key": "AWS_REGION", "value": "us-east-1", "set": true},
			},
		},
	})
	defer cleanup()

	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("3\n") // select codecommit (sorted position)
		w.WriteString("\n")  // keep current AWS_REGION (us-east-1)
		w.Close()
	}()

	out, err := runCmd(t, "config", "git", "setup")
	if err != nil {
		t.Fatalf("setup failed: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "CodeCommit") {
		t.Errorf("expected CodeCommit in output, got: %s", out)
	}
	// Should show current AWS_REGION value
	if !strings.Contains(out, "us-east-1") {
		t.Errorf("expected current AWS_REGION (us-east-1) to be shown, got: %s", out)
	}
	// Should show "keeping current value" since we pressed enter
	if !strings.Contains(out, "keeping current") {
		t.Errorf("expected 'keeping current value' message, got: %s", out)
	}
}

func TestConfigGitSetupSkippedVarShowsWarning(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
		"GET /workers/worker-1/env": map[string]any{
			"entries": []map[string]any{}, // nothing set
		},
	})
	defer cleanup()

	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("4\n") // select github
		w.WriteString("\n")  // skip GITHUB_TOKEN (empty answer, nothing current)
		w.Close()
	}()

	out, err := runCmd(t, "config", "git", "setup")
	if err != nil {
		t.Fatalf("setup failed: %v\noutput: %s", err, out)
	}

	// Should show skip warning with set-later command
	if !strings.Contains(out, "skipped") {
		t.Errorf("expected 'skipped' warning for empty GITHUB_TOKEN, got: %s", out)
	}
	if !strings.Contains(out, "worker-env set GITHUB_TOKEN") {
		t.Errorf("expected set-later command, got: %s", out)
	}
}

func TestConfigGitSwitchProvider(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
	})
	defer cleanup()

	// Set to github first
	_, err := runCmd(t, "config", "git", "set", "github", "--for-agent")
	if err != nil {
		t.Fatalf("first set failed: %v", err)
	}

	// Switch to gitlab
	_, err = runCmd(t, "config", "git", "set", "gitlab", "--for-agent")
	if err != nil {
		t.Fatalf("second set failed: %v", err)
	}

	// Verify show reflects gitlab
	out, err := runCmd(t, "config", "git", "show", "--for-agent")
	if err != nil {
		t.Fatalf("show failed: %v", err)
	}

	if !strings.Contains(out, "git_provider: gitlab") {
		t.Errorf("expected gitlab after switch, got: %s", out)
	}
	if !strings.Contains(out, "GITLAB_TOKEN") {
		t.Errorf("expected GITLAB_TOKEN after switch, got: %s", out)
	}
}

func TestConfigGitSetupPromptsRestart(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
		"GET /workers/worker-1/env": map[string]any{
			"entries": []map[string]any{},
		},
		"PUT /workers/worker-1/env/GITHUB_TOKEN": map[string]any{
			"key": "GITHUB_TOKEN",
		},
		"POST /workers/worker-1/restart": map[string]any{
			"status": "restarting",
		},
	})
	defer cleanup()

	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("4\n")             // select github
		w.WriteString("ghp_test123\n")   // set GITHUB_TOKEN
		w.WriteString("y\n")             // yes to restart
		w.Close()
	}()

	out, err := runCmd(t, "config", "git", "setup")
	if err != nil {
		t.Fatalf("setup failed: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "Restart worker") {
		t.Errorf("expected restart prompt, got: %s", out)
	}
	if !strings.Contains(out, "restarting") || !strings.Contains(out, "Worker") {
		t.Errorf("expected restart confirmation, got: %s", out)
	}
}

func TestConfigGitSetupDeclineRestart(t *testing.T) {
	_, cleanup := testServer(t, map[string]any{
		"/health": map[string]any{"status": "healthy", "version": "1.2.0"},
		"GET /workers/worker-1/env": map[string]any{
			"entries": []map[string]any{},
		},
		"PUT /workers/worker-1/env/GITHUB_TOKEN": map[string]any{
			"key": "GITHUB_TOKEN",
		},
	})
	defer cleanup()

	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("4\n")             // select github
		w.WriteString("ghp_test123\n")   // set GITHUB_TOKEN
		w.WriteString("n\n")             // no to restart
		w.Close()
	}()

	out, err := runCmd(t, "config", "git", "setup")
	if err != nil {
		t.Fatalf("setup failed: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "Remember to restart") {
		t.Errorf("expected 'remember to restart' message, got: %s", out)
	}
}
