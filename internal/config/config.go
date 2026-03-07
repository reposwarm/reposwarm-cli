// Package config manages CLI configuration stored in ~/.reposwarm/config.json.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitHub organization and repository constants.
// Change these in ONE place when the org or repo names change.
const (
	GitHubOrg     = "reposwarm"
	GitHubCLIRepo = "reposwarm-cli"
	GitHubAPIRepo = "reposwarm-api"
	GitHubUIRepo  = "reposwarm-ui"
	GitHubCore    = "reposwarm"

	OrgBaseURL = "https://github.com/" + GitHubOrg
	APIBaseURL = "https://api.github.com/repos/" + GitHubOrg

	DefaultCoreRepoURL = OrgBaseURL + "/" + GitHubCore + ".git"
	DefaultAPIRepoURL  = OrgBaseURL + "/" + GitHubAPIRepo + ".git"
	DefaultUIRepoURL   = OrgBaseURL + "/" + GitHubUIRepo + ".git"
	DefaultUIHubURL    = OrgBaseURL + "/" + GitHubUIRepo
	DefaultCLIRepoURL  = OrgBaseURL + "/" + GitHubCLIRepo

	CLIReleasesAPI  = APIBaseURL + "/" + GitHubCLIRepo + "/releases"
	CLIReleasesURL  = DefaultCLIRepoURL + "/releases"

	// Docker Compose constants.
	ComposeProjectName = "reposwarm"  // Docker Compose project name (container prefix)
	ComposeSubDir      = "temporal"   // Subdirectory within installDir for docker-compose.yml
)

// Config holds all CLI configuration.
type Config struct {
	APIUrl       string `json:"apiUrl"`
	APIToken     string `json:"apiToken"`
	Region       string `json:"region"`
	DefaultModel string `json:"defaultModel"`
	ChunkSize    int    `json:"chunkSize"`
	OutputFormat string `json:"outputFormat"`

	// Provider configuration
	ProviderConfig ProviderConfig `json:"providerConfig,omitempty"`

	// Git provider configuration
	GitProvider string `json:"gitProvider,omitempty"` // github, codecommit, gitlab, azure, bitbucket

	// Local setup defaults (used by 'reposwarm new --local' and guides)
	InstallType    string `json:"installType,omitempty"` // "docker" or "source"
	WorkerRepoURL  string `json:"workerRepoUrl,omitempty"`
	APIRepoURL     string `json:"apiRepoUrl,omitempty"`
	UIRepoURL      string `json:"uiRepoUrl,omitempty"`
	HubURL         string `json:"hubUrl,omitempty"`
	ArchHubURL     string `json:"archHubUrl,omitempty"`
	AskboxURL      string `json:"askboxUrl,omitempty"`
	DynamoDBTable  string `json:"dynamodbTable,omitempty"`
	TemporalPort   string `json:"temporalPort,omitempty"`
	TemporalUIPort string `json:"temporalUiPort,omitempty"`
	APIPort        string `json:"apiPort,omitempty"`
	UIPort         string `json:"uiPort,omitempty"`
	InstallDir     string `json:"installDir,omitempty"`
}

// Effective* methods return the configured value or the built-in default.

func (c *Config) EffectiveWorkerRepoURL() string {
	if c.WorkerRepoURL != "" { return c.WorkerRepoURL }
	return DefaultCoreRepoURL
}

func (c *Config) EffectiveAPIRepoURL() string {
	if c.APIRepoURL != "" { return c.APIRepoURL }
	return DefaultAPIRepoURL
}

func (c *Config) EffectiveUIRepoURL() string {
	if c.UIRepoURL != "" { return c.UIRepoURL }
	return DefaultUIRepoURL
}

func (c *Config) EffectiveHubURL() string {
	if c.HubURL != "" { return c.HubURL }
	return DefaultUIHubURL
}

func (c *Config) EffectiveDynamoDBTable() string {
	if c.DynamoDBTable != "" { return c.DynamoDBTable }
	return "reposwarm-cache"
}

func (c *Config) EffectiveModel() string {
	if c.DefaultModel != "" { return c.DefaultModel }
	return "us.anthropic.claude-sonnet-4-6"
}

func (c *Config) EffectiveTemporalPort() string {
	if c.TemporalPort != "" { return c.TemporalPort }
	return "7233"
}

func (c *Config) EffectiveTemporalUIPort() string {
	if c.TemporalUIPort != "" { return c.TemporalUIPort }
	return "8233"
}

func (c *Config) EffectiveAPIPort() string {
	if c.APIPort != "" { return c.APIPort }
	return "3000"
}

func (c *Config) EffectiveInstallDir() string {
	if c.InstallDir != "" { return c.InstallDir }
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".reposwarm")
}

func (c *Config) EffectiveUIPort() string {
	if c.UIPort != "" { return c.UIPort }
	return "3001"
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		APIUrl:       "http://localhost:3000/v1",
		Region:       "us-east-1",
		DefaultModel: "us.anthropic.claude-sonnet-4-6",
		ChunkSize:    10,
		OutputFormat: "pretty",
	}
}

// ValidKeys returns the list of settable config keys.
func ValidKeys() []string {
	return []string{
		"apiUrl", "apiToken", "region", "defaultModel", "chunkSize", "outputFormat",
		"installType", "workerRepoUrl", "apiRepoUrl", "uiRepoUrl", "hubUrl", "archHubUrl", "askboxUrl", "dynamodbTable",
		"temporalPort", "temporalUiPort", "apiPort", "uiPort", "installDir",
		"provider", "awsRegion", "proxyUrl", "proxyKey", "smallModel",
	}
}

// ConfigDir returns the config directory path.
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".reposwarm"), nil
}

// ConfigPath returns the config file path.
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads config from disk, falling back to defaults.
// Environment variables REPOSWARM_API_URL and REPOSWARM_API_TOKEN override file values.
func Load() (*Config, error) {
	cfg := DefaultConfig()

	path, err := ConfigPath()
	if err != nil {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvOverrides(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("REPOSWARM_API_URL"); v != "" {
		cfg.APIUrl = v
	}
	if v := os.Getenv("REPOSWARM_API_TOKEN"); v != "" {
		cfg.APIToken = v
	}
}

// Save writes config to disk.
func Save(cfg *Config) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	path := filepath.Join(dir, "config.json")
	return os.WriteFile(path, data, 0600)
}

// Set updates a single config key.
func Set(cfg *Config, key, value string) error {
	switch key {
	case "apiUrl":
		cfg.APIUrl = value
	case "apiToken":
		cfg.APIToken = value
	case "region":
		cfg.Region = value
	case "defaultModel":
		cfg.DefaultModel = value
	case "chunkSize":
		var n int
		if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
			return fmt.Errorf("chunkSize must be a number")
		}
		cfg.ChunkSize = n
	case "outputFormat":
		if value != "pretty" && value != "json" {
			return fmt.Errorf("outputFormat must be 'pretty' or 'json'")
		}
		cfg.OutputFormat = value
	case "workerRepoUrl":
		cfg.WorkerRepoURL = value
	case "apiRepoUrl":
		cfg.APIRepoURL = value
	case "uiRepoUrl":
		cfg.UIRepoURL = value
	case "hubUrl":
		cfg.HubURL = value
	case "archHubUrl":
		cfg.ArchHubURL = value
	case "askboxUrl":
		cfg.AskboxURL = value
	case "dynamodbTable":
		cfg.DynamoDBTable = value
	case "temporalPort":
		cfg.TemporalPort = value
	case "temporalUiPort":
		cfg.TemporalUIPort = value
	case "apiPort":
		cfg.APIPort = value
	case "uiPort":
		cfg.UIPort = value
	case "installDir":
		cfg.InstallDir = value
	case "installType":
		if value != "docker" && value != "source" {
			return fmt.Errorf("installType must be 'docker' or 'source'")
		}
		cfg.InstallType = value
	case "provider":
		if !IsValidProvider(value) {
			return fmt.Errorf("unknown provider: %s (valid: anthropic, bedrock, litellm)", value)
		}
		cfg.ProviderConfig.Provider = Provider(value)
	case "awsRegion":
		cfg.ProviderConfig.AWSRegion = value
	case "proxyUrl":
		cfg.ProviderConfig.ProxyURL = value
	case "proxyKey":
		cfg.ProviderConfig.ProxyKey = value
	case "smallModel":
		cfg.ProviderConfig.SmallModel = value

	default:
		return fmt.Errorf("unknown config key: %s (valid: %s)", key, strings.Join(ValidKeys(), ", "))
	}
	return nil
}

// MaskedToken returns a token with most characters replaced by *.
func MaskedToken(token string) string {
	if len(token) <= 8 {
		return "***"
	}
	return "***..." + token[len(token)-6:]
}

// EffectiveProvider returns the configured provider or the default.
func (c *Config) EffectiveProvider() Provider {
	if c.ProviderConfig.Provider != "" {
		return c.ProviderConfig.Provider
	}
	return ProviderAnthropic
}

// IsDockerInstall returns true if the install type is Docker Compose.
func (c *Config) IsDockerInstall() bool {
	return c.InstallType == "docker"
}
