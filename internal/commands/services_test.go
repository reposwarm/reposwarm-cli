package commands

import (
	"testing"
)

func TestIsKnownService(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		want        bool
	}{
		{
			name:        "api is known",
			serviceName: "api",
			want:        true,
		},
		{
			name:        "worker is known",
			serviceName: "worker",
			want:        true,
		},
		{
			name:        "temporal is known",
			serviceName: "temporal",
			want:        true,
		},
		{
			name:        "ui is known",
			serviceName: "ui",
			want:        true,
		},
		{
			name:        "worker with suffix is known",
			serviceName: "worker-1",
			want:        true,
		},
		{
			name:        "unknown service",
			serviceName: "unknown",
			want:        false,
		},
		{
			name:        "empty string",
			serviceName: "",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isKnownService(tt.serviceName)
			if got != tt.want {
				t.Errorf("isKnownService(%q) = %v, want %v", tt.serviceName, got, tt.want)
			}
		})
	}
}

func TestStopCommand_Args(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "no args should be valid",
			args:        []string{},
			shouldError: false,
		},
		{
			name:        "one valid arg should be valid",
			args:        []string{"worker"},
			shouldError: false,
		},
		{
			name:        "two args should error",
			args:        []string{"worker", "api"},
			shouldError: true,
			errorMsg:    "Too many arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newStopCmd()
			cmd.SetArgs(tt.args)

			// We can't fully execute the command in tests without mocking,
			// but we can test argument validation
			err := cmd.Args(cmd, tt.args)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestKnownServices(t *testing.T) {
	// Test that knownServices contains expected values
	expected := map[string]bool{
		"api":      true,
		"worker":   true,
		"temporal": true,
		"ui":       true,
	}

	if len(knownServices) != len(expected) {
		t.Errorf("Expected %d known services, got %d", len(expected), len(knownServices))
	}

	for _, svc := range knownServices {
		if !expected[svc] {
			t.Errorf("Unexpected service in knownServices: %q", svc)
		}
	}

	for svc := range expected {
		found := false
		for _, known := range knownServices {
			if known == svc {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected service %q not found in knownServices", svc)
		}
	}
}
