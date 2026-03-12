package commands

import (
	"testing"
)

// TestLogsFlagAliases verifies both --tail and --lines are registered as flags.
func TestLogsFlagAliases(t *testing.T) {
	root := NewRootCmd("test")

	logsCmd, _, _ := root.Find([]string{"logs"})
	if logsCmd == nil {
		t.Fatal("could not find 'logs' command")
	}

	tests := []struct {
		name     string
		flagName string
	}{
		{"tail flag exists", "tail"},
		{"lines alias exists", "lines"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := logsCmd.Flags().Lookup(tc.flagName)
			if f == nil {
				t.Errorf("flag --%s not registered on 'logs'", tc.flagName)
			}
		})
	}
}
