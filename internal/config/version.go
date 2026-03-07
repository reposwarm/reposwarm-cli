package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Minimum compatible versions for CLI ↔ API ↔ UI.
// Bump these when the CLI starts using new API/UI features.
const (
	MinAPIVersion = "1.0.0" // lowered: all features work with v1.0.0 Docker image
	MinUIVersion  = "1.0.0" // no specific requirements yet
)

// SemVer represents a parsed semantic version.
type SemVer struct {
	Major int
	Minor int
	Patch int
	Raw   string
}

// ParseSemVer parses a version string like "1.2.3" or "v1.2.3".
func ParseSemVer(s string) (SemVer, error) {
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) < 2 {
		return SemVer{Raw: s}, fmt.Errorf("invalid version: %s", s)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return SemVer{Raw: s}, fmt.Errorf("invalid major version: %s", parts[0])
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return SemVer{Raw: s}, fmt.Errorf("invalid minor version: %s", parts[1])
	}

	patch := 0
	if len(parts) >= 3 {
		// Handle "3-beta" or "3.foo" suffixes
		patchStr := parts[2]
		if idx := strings.IndexAny(patchStr, "-+"); idx >= 0 {
			patchStr = patchStr[:idx]
		}
		patch, _ = strconv.Atoi(patchStr)
	}

	return SemVer{Major: major, Minor: minor, Patch: patch, Raw: s}, nil
}

// IsCompatible checks if 'actual' meets the 'minimum' version requirement.
// Returns true if actual >= minimum.
func IsCompatible(actual, minimum string) bool {
	a, err := ParseSemVer(actual)
	if err != nil {
		return false
	}
	m, err := ParseSemVer(minimum)
	if err != nil {
		return true // can't parse minimum, assume compatible
	}

	if a.Major != m.Major {
		return a.Major > m.Major
	}
	if a.Minor != m.Minor {
		return a.Minor > m.Minor
	}
	return a.Patch >= m.Patch
}

// VersionCheckResult holds the result of a version compatibility check.
type VersionCheckResult struct {
	Component  string // "api" or "ui"
	Actual     string // version reported by the component
	Minimum    string // minimum required by CLI
	Compatible bool
}

// CheckVersions compares actual versions against minimums.
func CheckVersions(apiVersion, uiVersion string) []VersionCheckResult {
	var results []VersionCheckResult

	if apiVersion != "" {
		results = append(results, VersionCheckResult{
			Component:  "API Server",
			Actual:     apiVersion,
			Minimum:    MinAPIVersion,
			Compatible: IsCompatible(apiVersion, MinAPIVersion),
		})
	}

	if uiVersion != "" {
		results = append(results, VersionCheckResult{
			Component:  "UI",
			Actual:     uiVersion,
			Minimum:    MinUIVersion,
			Compatible: IsCompatible(uiVersion, MinUIVersion),
		})
	}

	return results
}
