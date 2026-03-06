package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidGitProviders(t *testing.T) {
	ResetProvidersCache()
	providers := ValidGitProviders()

	if len(providers) == 0 {
		t.Fatal("ValidGitProviders returned empty list")
	}

	expected := map[string]bool{
		"github":     false,
		"codecommit": false,
		"gitlab":     false,
		"azure":      false,
		"bitbucket":  false,
	}

	for _, p := range providers {
		if _, ok := expected[p]; ok {
			expected[p] = true
		} else {
			t.Errorf("unexpected git provider: %s", p)
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing expected git provider: %s", name)
		}
	}
}

func TestGetGitProviderBundle(t *testing.T) {
	ResetProvidersCache()

	tests := []struct {
		name       string
		provider   string
		wantLabel  string
		wantEnvKey string
		wantErr    bool
	}{
		{
			name:       "github",
			provider:   "github",
			wantLabel:  "GitHub",
			wantEnvKey: "GITHUB_TOKEN",
		},
		{
			name:       "codecommit",
			provider:   "codecommit",
			wantLabel:  "AWS CodeCommit",
			wantEnvKey: "AWS_REGION",
		},
		{
			name:       "gitlab",
			provider:   "gitlab",
			wantLabel:  "GitLab",
			wantEnvKey: "GITLAB_TOKEN",
		},
		{
			name:       "azure",
			provider:   "azure",
			wantLabel:  "Azure DevOps",
			wantEnvKey: "AZURE_DEVOPS_PAT",
		},
		{
			name:       "bitbucket",
			provider:   "bitbucket",
			wantLabel:  "Bitbucket",
			wantEnvKey: "BITBUCKET_USERNAME",
		},
		{
			name:     "unknown provider",
			provider: "svn",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetProvidersCache()
			b, err := GetGitProviderBundle(tt.provider)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if b.Label != tt.wantLabel {
				t.Errorf("label = %q, want %q", b.Label, tt.wantLabel)
			}
			if len(b.EnvVars) == 0 {
				t.Error("expected at least one env var")
			}
			found := false
			for _, ev := range b.EnvVars {
				if ev.Key == tt.wantEnvKey {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected env var %s not found in bundle", tt.wantEnvKey)
			}
		})
	}
}

func TestGitProviderEnvVars(t *testing.T) {
	ResetProvidersCache()

	tests := []struct {
		name        string
		gitProvider string
		wantKeys    []string
		wantEmpty   bool
	}{
		{
			name:        "github requires GITHUB_TOKEN",
			gitProvider: "github",
			wantKeys:    []string{"GITHUB_TOKEN"},
		},
		{
			name:        "gitlab requires GITLAB_TOKEN",
			gitProvider: "gitlab",
			wantKeys:    []string{"GITLAB_TOKEN"},
		},
		{
			name:        "azure requires PAT and ORG",
			gitProvider: "azure",
			wantKeys:    []string{"AZURE_DEVOPS_PAT", "AZURE_DEVOPS_ORG"},
		},
		{
			name:        "bitbucket requires username and app password",
			gitProvider: "bitbucket",
			wantKeys:    []string{"BITBUCKET_USERNAME", "BITBUCKET_APP_PASSWORD"},
		},
		{
			name:        "codecommit requires AWS_REGION",
			gitProvider: "codecommit",
			wantKeys:    []string{"AWS_REGION"},
		},
		{
			name:        "empty provider returns nil",
			gitProvider: "",
			wantEmpty:   true,
		},
		{
			name:        "unknown provider returns nil",
			gitProvider: "mercurial",
			wantEmpty:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetProvidersCache()
			reqs := GitProviderEnvVars(tt.gitProvider)
			if tt.wantEmpty {
				if len(reqs) != 0 {
					t.Errorf("expected empty, got %d env vars", len(reqs))
				}
				return
			}
			keys := make(map[string]bool)
			for _, r := range reqs {
				keys[r.Key] = true
			}
			for _, wk := range tt.wantKeys {
				if !keys[wk] {
					t.Errorf("missing expected env var: %s (got: %v)", wk, keys)
				}
			}
		})
	}
}

func TestRequiredEnvVarsWithGit(t *testing.T) {
	ResetProvidersCache()

	tests := []struct {
		name        string
		provider    Provider
		authMethod  BedrockAuthMethod
		gitProvider string
		wantKeys    []string
		wantAbsent  []string
	}{
		{
			name:        "bedrock + iam-role + github",
			provider:    ProviderBedrock,
			authMethod:  BedrockAuthIAMRole,
			gitProvider: "github",
			wantKeys:    []string{"CLAUDE_CODE_USE_BEDROCK", "AWS_REGION", "ANTHROPIC_MODEL", "GITHUB_TOKEN"},
			wantAbsent:  []string{"GITLAB_TOKEN", "AZURE_DEVOPS_PAT"},
		},
		{
			name:        "bedrock + iam-role + gitlab",
			provider:    ProviderBedrock,
			authMethod:  BedrockAuthIAMRole,
			gitProvider: "gitlab",
			wantKeys:    []string{"CLAUDE_CODE_USE_BEDROCK", "AWS_REGION", "ANTHROPIC_MODEL", "GITLAB_TOKEN"},
			wantAbsent:  []string{"GITHUB_TOKEN"},
		},
		{
			name:        "anthropic + github",
			provider:    ProviderAnthropic,
			gitProvider: "github",
			wantKeys:    []string{"ANTHROPIC_API_KEY", "ANTHROPIC_MODEL", "GITHUB_TOKEN"},
		},
		{
			name:        "bedrock + no git provider",
			provider:    ProviderBedrock,
			authMethod:  BedrockAuthIAMRole,
			gitProvider: "",
			wantKeys:    []string{"CLAUDE_CODE_USE_BEDROCK", "AWS_REGION", "ANTHROPIC_MODEL"},
			wantAbsent:  []string{"GITHUB_TOKEN", "GITLAB_TOKEN"},
		},
		{
			name:        "bedrock + api-keys + azure",
			provider:    ProviderBedrock,
			authMethod:  BedrockAuthAPIKeys,
			gitProvider: "azure",
			wantKeys:    []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AZURE_DEVOPS_PAT", "AZURE_DEVOPS_ORG"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetProvidersCache()
			pc := &ProviderConfig{
				Provider:    tt.provider,
				BedrockAuth: tt.authMethod,
			}
			reqs := RequiredEnvVarsWithGit(pc, tt.gitProvider)
			keys := make(map[string]bool)
			for _, r := range reqs {
				keys[r.Key] = true
			}
			for _, wk := range tt.wantKeys {
				if !keys[wk] {
					t.Errorf("missing expected env var: %s (got keys: %v)", wk, keysSlice(keys))
				}
			}
			for _, absent := range tt.wantAbsent {
				if keys[absent] {
					t.Errorf("unexpected env var present: %s", absent)
				}
			}
		})
	}
}

func TestGitProviderInConfig(t *testing.T) {
	// Create a temp config with gitProvider
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, ".reposwarm")
	os.MkdirAll(cfgDir, 0755)

	cfgPath := filepath.Join(cfgDir, "config.json")
	os.WriteFile(cfgPath, []byte(`{
		"apiUrl": "http://localhost:3000/v1",
		"apiToken": "test-token",
		"providerConfig": {"provider": "bedrock", "bedrockAuth": "iam-role"},
		"gitProvider": "github"
	}`), 0644)

	// Parse
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	var cfg Config
	if err := parseJSON(data, &cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.GitProvider != "github" {
		t.Errorf("gitProvider = %q, want 'github'", cfg.GitProvider)
	}
}

func TestLoadEmbeddedProviders(t *testing.T) {
	pf, err := loadEmbeddedProviders()
	if err != nil {
		t.Fatal(err)
	}

	if len(pf.Providers) == 0 {
		t.Error("expected providers, got none")
	}

	if len(pf.GitProviders) == 0 {
		t.Error("expected git providers, got none")
	}

	// Verify all 5 git providers
	expected := []string{"github", "codecommit", "gitlab", "azure", "bitbucket"}
	for _, name := range expected {
		if _, ok := pf.GitProviders[name]; !ok {
			t.Errorf("missing git provider in embedded bundle: %s", name)
		}
	}
}

func TestGitProviderHintsPresent(t *testing.T) {
	ResetProvidersCache()

	for _, name := range []string{"github", "codecommit", "gitlab", "azure", "bitbucket"} {
		b, err := GetGitProviderBundle(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if b.Hint == "" {
			t.Errorf("%s: missing hint", name)
		}
		if b.Label == "" {
			t.Errorf("%s: missing label", name)
		}
	}
}

func keysSlice(m map[string]bool) []string {
	s := make([]string, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	return s
}

func parseJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
