package config

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed providers.json
var embeddedProviders embed.FS

// ProviderBundle describes a single provider's configuration from providers.json.
type ProviderBundle struct {
	Label    string            `json:"label"`
	EnvVars  ProviderEnvConfig `json:"envVars"`
	Models   map[string]string `json:"models"`
	Default  string            `json:"defaultModel"`
	SmallMod string            `json:"defaultSmallModel"`
	PinVars  map[string]string `json:"pinVars,omitempty"`
}

// ProviderEnvConfig holds the env var configuration for a provider.
type ProviderEnvConfig struct {
	Always            []EnvVarDef               `json:"always"`
	AuthMethods       map[string]AuthMethodDef   `json:"authMethods,omitempty"`
	DefaultAuthMethod string                     `json:"defaultAuthMethod,omitempty"`
}

// AuthMethodDef defines an auth method and its required env vars.
type AuthMethodDef struct {
	Label   string      `json:"label"`
	EnvVars []EnvVarDef `json:"envVars"`
}

// EnvVarDef describes an environment variable requirement in the JSON file.
type EnvVarDef struct {
	Key      string   `json:"key"`
	Desc     string   `json:"desc"`
	Required bool     `json:"required"`
	Value    string   `json:"value,omitempty"`    // fixed value (e.g. CLAUDE_CODE_USE_BEDROCK=1)
	Alts     []string `json:"alts,omitempty"`     // alternative key names
	Secret   bool     `json:"secret,omitempty"`   // true = sensitive, don't display value
}

// ProvidersFile is the top-level structure of providers.json.
type ProvidersFile struct {
	Providers    map[string]ProviderBundle `json:"providers"`
	CommonEnvVars []EnvVarDef             `json:"commonEnvVars"`
	KnownEnvVars  []string                `json:"knownEnvVars"`
}

var cachedProviders *ProvidersFile

// LoadProviders loads the provider configuration from external file or embedded default.
// Lookup order: ~/.reposwarm/providers.json → embedded providers.json
func LoadProviders() (*ProvidersFile, error) {
	if cachedProviders != nil {
		return cachedProviders, nil
	}

	// Try external file first (~/.reposwarm/providers.json)
	home, err := os.UserHomeDir()
	if err == nil {
		extPath := filepath.Join(home, ".reposwarm", "providers.json")
		if data, err := os.ReadFile(extPath); err == nil {
			var pf ProvidersFile
			if err := json.Unmarshal(data, &pf); err == nil {
				cachedProviders = &pf
				return cachedProviders, nil
			}
			// Invalid JSON in external file — fall through to embedded
		}
	}

	// Fall back to embedded
	data, err := embeddedProviders.ReadFile("providers.json")
	if err != nil {
		return nil, fmt.Errorf("reading embedded providers.json: %w", err)
	}

	var pf ProvidersFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("parsing embedded providers.json: %w", err)
	}

	cachedProviders = &pf
	return cachedProviders, nil
}

// ResetProvidersCache clears the cached providers (for testing).
func ResetProvidersCache() {
	cachedProviders = nil
}

// GetProviderBundle returns the bundle for a given provider name.
func GetProviderBundle(provider Provider) (*ProviderBundle, error) {
	pf, err := LoadProviders()
	if err != nil {
		return nil, err
	}
	b, ok := pf.Providers[string(provider)]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}
	return &b, nil
}

// GetAuthMethods returns the auth method definitions for a provider (e.g. bedrock).
func GetAuthMethods(provider Provider) (map[string]AuthMethodDef, string) {
	b, err := GetProviderBundle(provider)
	if err != nil || b.EnvVars.AuthMethods == nil {
		return nil, ""
	}
	return b.EnvVars.AuthMethods, b.EnvVars.DefaultAuthMethod
}
