package config

import "testing"

func TestParseSemVer(t *testing.T) {
	tests := []struct {
		input   string
		major   int
		minor   int
		patch   int
		wantErr bool
	}{
		{"1.0.0", 1, 0, 0, false},
		{"1.1.0", 1, 1, 0, false},
		{"2.3.14", 2, 3, 14, false},
		{"v1.3.49", 1, 3, 49, false},
		{"1.0", 1, 0, 0, false},
		{"1.0.0-beta", 1, 0, 0, false},
		{"bad", 0, 0, 0, true},
	}

	for _, tt := range tests {
		v, err := ParseSemVer(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseSemVer(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if v.Major != tt.major || v.Minor != tt.minor || v.Patch != tt.patch {
			t.Errorf("ParseSemVer(%q) = %d.%d.%d, want %d.%d.%d", tt.input, v.Major, v.Minor, v.Patch, tt.major, tt.minor, tt.patch)
		}
	}
}

func TestIsCompatible(t *testing.T) {
	tests := []struct {
		actual   string
		minimum  string
		want     bool
	}{
		{"1.1.0", "1.1.0", true},   // exact match
		{"1.2.0", "1.1.0", true},   // newer minor
		{"2.0.0", "1.1.0", true},   // newer major
		{"1.1.1", "1.1.0", true},   // newer patch
		{"1.0.0", "1.1.0", false},  // older minor
		{"0.9.0", "1.0.0", false},  // older major
		{"1.0.9", "1.1.0", false},  // higher patch but lower minor
		{"1.3.49", "1.1.0", true},  // CLI version >> API minimum
	}

	for _, tt := range tests {
		got := IsCompatible(tt.actual, tt.minimum)
		if got != tt.want {
			t.Errorf("IsCompatible(%q, %q) = %v, want %v", tt.actual, tt.minimum, got, tt.want)
		}
	}
}

func TestCheckVersions(t *testing.T) {
	results := CheckVersions("1.2.0", "1.0.0")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if !results[0].Compatible {
		t.Error("API 1.2.0 should be compatible with min 1.2.0")
	}
	if !results[1].Compatible {
		t.Error("UI 1.0.0 should be compatible with min 1.0.0")
	}

	results = CheckVersions("1.0.0", "")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Compatible {
		t.Error("API 1.0.0 should be compatible with min 1.0.0")
	}
}
