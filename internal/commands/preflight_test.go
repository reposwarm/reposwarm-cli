package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestCheckRepoAccessLooksUpURL verifies that checkRepoAccess fetches the
// repo's URL from the API when given a name without owner/repo format.
func TestCheckRepoAccessLooksUpURL(t *testing.T) {
	// Set up a mock API server that returns repo info with a GitHub URL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			w.WriteHeader(401)
			return
		}

		switch r.URL.Path {
		case "/repos/is-odd":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"name":   "is-odd",
					"source": "GitHub",
					"url":    "https://github.com/jonschlinkert/is-odd",
				},
			})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	// Configure the CLI to use our test server
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	cfgDir := dir + "/.reposwarm"
	os.MkdirAll(cfgDir, 0700)
	cfg := map[string]any{
		"apiUrl":   server.URL,
		"apiToken": "test-token",
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(cfgDir+"/config.json", data, 0600)

	// Call checkRepoAccess with just "is-odd" (no owner/repo)
	check := checkRepoAccess("is-odd")

	// It should NOT warn "no owner/repo format" — it should look up the URL
	if check.Status == "warn" && check.Message == "'is-odd' — cannot verify accessibility (no owner/repo format)" {
		t.Errorf("checkRepoAccess did not look up repo URL from API; got warn: %s", check.Message)
	}
}

// TestCheckRepoAccessOwnerRepoFormat verifies that owner/repo format works directly.
func TestCheckRepoAccessOwnerRepoFormat(t *testing.T) {
	// This test just verifies the function handles owner/repo format
	// (it may fail or succeed depending on network, but shouldn't warn about format)
	check := checkRepoAccess("jonschlinkert/is-odd")
	if check.Status == "warn" && check.Message == "'jonschlinkert/is-odd' — cannot verify accessibility (no owner/repo format)" {
		t.Error("should not warn about format for owner/repo input")
	}
}
