package config

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
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

// GitProviderBundle describes a git provider's configuration.
type GitProviderBundle struct {
	Label    string      `json:"label"`
	EnvVars  []EnvVarDef `json:"envVars"`
	AuthNote string      `json:"authNote,omitempty"`
	Hint     string      `json:"hint,omitempty"`
}

// ProvidersFile is the top-level structure of providers.json.
type ProvidersFile struct {
	Providers     map[string]ProviderBundle    `json:"providers"`
	CommonEnvVars []EnvVarDef                  `json:"commonEnvVars"`
	GitProviders  map[string]GitProviderBundle `json:"gitProviders"`
	KnownEnvVars  []string                     `json:"knownEnvVars"`
}

var cachedProviders *ProvidersFile

// LoadProviders loads the provider configuration.
// Lookup order: API server → ~/.reposwarm/providers.json → embedded providers.json
func LoadProviders() (*ProvidersFile, error) {
	if cachedProviders != nil {
		return cachedProviders, nil
	}

	// Try API server first (single source of truth)
	if pf := fetchProvidersFromAPI(); pf != nil {
		cachedProviders = pf
		return cachedProviders, nil
	}

	// Try external file (~/.reposwarm/providers.json)
	home, err := os.UserHomeDir()
	if err == nil {
		extPath := filepath.Join(home, ".reposwarm", "providers.json")
		if data, err := os.ReadFile(extPath); err == nil {
			var pf ProvidersFile
			if err := json.Unmarshal(data, &pf); err == nil {
				cachedProviders = &pf
				return cachedProviders, nil
			}
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

// fetchProvidersFromAPI tries to fetch the providers bundle from the API server.
// Returns nil if the API is unavailable or doesn't support the endpoint.
func fetchProvidersFromAPI() *ProvidersFile {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	// Read config to get API URL and token
	cfgPath := filepath.Join(home, ".reposwarm", "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil
	}

	var cfg struct {
		APIUrl   string `json:"apiUrl"`
		APIToken string `json:"apiToken"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil || cfg.APIUrl == "" {
		return nil
	}

	url := cfg.APIUrl + "/providers"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil
	}
	if cfg.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var wrapper struct {
		Data ProvidersFile `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil
	}

	// Validate we got something useful
	if len(wrapper.Data.Providers) == 0 {
		return nil
	}

	return &wrapper.Data
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

// ValidGitProviders returns the list of supported git provider names.
func ValidGitProviders() []string {
	pf, err := LoadProviders()
	if err != nil {
		return []string{"github", "codecommit", "gitlab", "azure", "bitbucket"}
	}
	names := make([]string, 0, len(pf.GitProviders))
	for k := range pf.GitProviders {
		names = append(names, k)
	}
	return names
}

// GetGitProviderBundle returns the bundle for a given git provider.
func GetGitProviderBundle(name string) (*GitProviderBundle, error) {
	pf, err := LoadProviders()
	if err != nil {
		return nil, err
	}
	b, ok := pf.GitProviders[name]
	if !ok {
		return nil, fmt.Errorf("unknown git provider: %s", name)
	}
	return &b, nil
}

// GitProviderEnvVars returns the env var requirements for the configured git provider.
func GitProviderEnvVars(gitProvider string) []EnvRequirement {
	if gitProvider == "" {
		return nil
	}
	b, err := GetGitProviderBundle(gitProvider)
	if err != nil {
		return nil
	}
	var reqs []EnvRequirement
	for _, ev := range b.EnvVars {
		reqs = append(reqs, EnvRequirement{Key: ev.Key, Desc: ev.Desc, Required: ev.Required, Alts: ev.Alts})
	}
	return reqs
}
