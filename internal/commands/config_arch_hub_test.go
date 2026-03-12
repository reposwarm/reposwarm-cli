package commands

import (
	"strings"
	"testing"
)

func TestConfigArchHubShowCmd(t *testing.T) {
	// Set up a fake home with a config file
	_, cleanup := testServer(t, nil)
	defer cleanup()

	// The command should exist and run without error (even if no worker.env exists)
	out, err := runCmd(t, "config", "arch-hub", "show", "--json")
	if err != nil {
		t.Fatalf("config arch-hub show --json: %v", err)
	}
	// Should contain mode field
	if !strings.Contains(out, `"mode"`) {
		t.Errorf("output should contain mode field, got: %s", out)
	}
}

func TestConfigArchHubLocalValidatesPath(t *testing.T) {
	_, cleanup := testServer(t, nil)
	defer cleanup()

	// "local" without a path argument should fail
	_, err := runCmd(t, "config", "arch-hub", "local")
	if err == nil {
		t.Error("expected error when no path argument provided")
	}
}

func TestConfigArchHubGitHubHasFlags(t *testing.T) {
	root := NewRootCmd("test")

	// Navigate to the github subcommand
	configCmd, _, _ := root.Find([]string{"config", "arch-hub", "github"})
	if configCmd == nil {
		t.Fatal("config arch-hub github command not found")
	}

	// Verify flags exist
	for _, flagName := range []string{"url", "repo", "token"} {
		f := configCmd.Flags().Lookup(flagName)
		if f == nil {
			t.Errorf("flag --%s not found on config arch-hub github", flagName)
		}
	}
}

func TestConfigArchHubGitHubRequiresURL(t *testing.T) {
	_, cleanup := testServer(t, nil)
	defer cleanup()

	// github without --url should fail
	_, err := runCmd(t, "config", "arch-hub", "github")
	if err == nil {
		t.Error("expected error when --url not provided")
	}
}

func TestConfigArchHubSubcommandsRegistered(t *testing.T) {
	root := NewRootCmd("test")

	// Find the config arch-hub command
	configCmd, _, _ := root.Find([]string{"config", "arch-hub"})
	if configCmd == nil {
		t.Fatal("config arch-hub command not found")
	}

	expected := map[string]bool{"local": false, "github": false, "show": false}
	for _, sub := range configCmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("subcommand %q not registered under config arch-hub", name)
		}
	}
}
