package config

import "strings"

// Provider represents an LLM provider backend.
type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderBedrock   Provider = "bedrock"
	ProviderLiteLLM   Provider = "litellm"
)

// BedrockAuthMethod represents AWS authentication methods for Bedrock.
type BedrockAuthMethod string

const (
	BedrockAuthIAMRole      BedrockAuthMethod = "iam-role"       // EC2 instance profile / ECS task role
	BedrockAuthLongTermKeys BedrockAuthMethod = "long-term-keys" // AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY
	BedrockAuthSSO          BedrockAuthMethod = "sso"            // AWS SSO / Identity Center
	BedrockAuthProfile      BedrockAuthMethod = "profile"        // Named AWS profile (~/.aws/credentials)
)

// ValidProviders returns all supported provider names.
func ValidProviders() []string {
	return []string{string(ProviderAnthropic), string(ProviderBedrock), string(ProviderLiteLLM)}
}

// IsValidProvider returns true if the provider name is known.
func IsValidProvider(p string) bool {
	switch Provider(p) {
	case ProviderAnthropic, ProviderBedrock, ProviderLiteLLM:
		return true
	}
	return false
}

// ProviderConfig holds provider-specific configuration.
type ProviderConfig struct {
	Provider     Provider          `json:"provider,omitempty"`
	AWSRegion    string            `json:"awsRegion,omitempty"`
	BedrockAuth  BedrockAuthMethod `json:"bedrockAuth,omitempty"`  // AWS auth method for Bedrock
	AWSProfile   string            `json:"awsProfile,omitempty"`   // for "profile" auth method
	ProxyURL     string            `json:"proxyUrl,omitempty"`     // LiteLLM proxy URL
	ProxyKey     string            `json:"proxyKey,omitempty"`     // LiteLLM proxy API key
	SmallModel   string            `json:"smallModel,omitempty"`   // Fast/cheap model for triage
	ModelPins    map[string]string `json:"modelPins,omitempty"`    // alias → pinned model ID
}

// ModelAlias maps human-friendly names to provider-specific model IDs.
type ModelAlias struct {
	Alias     string
	Anthropic string
	Bedrock   string
}

// KnownAliases returns the standard model alias table.
func KnownAliases() []ModelAlias {
	return []ModelAlias{
		{"sonnet", "claude-sonnet-4-6", "us.anthropic.claude-sonnet-4-6"},
		{"opus", "claude-opus-4-6", "us.anthropic.claude-opus-4-6-v1"},
		{"haiku", "claude-haiku-4-5", "us.anthropic.claude-haiku-4-5-20251001-v1:0"},
		{"sonnet-3.5", "claude-3-5-sonnet-20241022", "us.anthropic.claude-3-5-sonnet-20241022-v2:0"},
	}
}

// ResolveModel takes an alias or raw model ID and returns the provider-specific model ID.
// If modelPins has a pin for this alias, use it. Otherwise resolve from the alias table.
func ResolveModel(alias string, provider Provider, pins map[string]string) string {
	// Check pins first
	if pins != nil {
		if pinned, ok := pins[alias]; ok {
			return pinned
		}
	}

	// Check alias table
	for _, a := range KnownAliases() {
		if a.Alias == alias {
			switch provider {
			case ProviderBedrock:
				return a.Bedrock
			case ProviderAnthropic, ProviderLiteLLM:
				return a.Anthropic
			}
		}
	}

	// Not an alias — return as-is (raw model ID)
	return alias
}

// DefaultSmallModel returns the default small/fast model for a provider.
func DefaultSmallModel(provider Provider) string {
	switch provider {
	case ProviderBedrock:
		return "us.anthropic.claude-haiku-4-5-20251001-v1:0"
	default:
		return "claude-haiku-4-5"
	}
}

// WorkerEnvVars returns the env vars the worker needs for a given provider config.
func WorkerEnvVars(pc *ProviderConfig, model string) map[string]string {
	vars := map[string]string{}

	resolved := ResolveModel(model, pc.Provider, pc.ModelPins)
	smallResolved := pc.SmallModel
	if smallResolved == "" {
		smallResolved = DefaultSmallModel(pc.Provider)
	}

	switch pc.Provider {
	case ProviderBedrock:
		vars["CLAUDE_CODE_USE_BEDROCK"] = "1"
		vars["AWS_REGION"] = pc.AWSRegion
		if pc.AWSRegion == "" {
			vars["AWS_REGION"] = "us-east-1"
		}
		vars["ANTHROPIC_MODEL"] = resolved
		vars["ANTHROPIC_SMALL_FAST_MODEL"] = smallResolved

		// Set auth method specific env vars
		switch pc.BedrockAuth {
		case BedrockAuthLongTermKeys:
			// Keys are stored separately, not in config
			// The CLI will set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY directly
		case BedrockAuthSSO, BedrockAuthProfile:
			if pc.AWSProfile != "" {
				vars["AWS_PROFILE"] = pc.AWSProfile
			}
		case BedrockAuthIAMRole, "":
			// IAM role auth - no extra env vars needed (inherited from instance/task)
		}

		// Set version pins if available
		if pc.ModelPins != nil {
			if v, ok := pc.ModelPins["opus"]; ok {
				vars["ANTHROPIC_DEFAULT_OPUS_MODEL"] = v
			}
			if v, ok := pc.ModelPins["sonnet"]; ok {
				vars["ANTHROPIC_DEFAULT_SONNET_MODEL"] = v
			}
			if v, ok := pc.ModelPins["haiku"]; ok {
				vars["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = v
			}
		}

	case ProviderLiteLLM:
		// LiteLLM proxy uses standard Anthropic SDK format but routes through proxy
		vars["ANTHROPIC_API_KEY"] = pc.ProxyKey
		vars["ANTHROPIC_BASE_URL"] = pc.ProxyURL
		vars["ANTHROPIC_MODEL"] = resolved

	case ProviderAnthropic:
		// Standard Anthropic — API key should already be set
		vars["ANTHROPIC_MODEL"] = resolved
		vars["ANTHROPIC_SMALL_FAST_MODEL"] = smallResolved
	}

	return vars
}

// EnvRequirement describes an environment variable requirement.
type EnvRequirement struct {
	Key      string   // Environment variable name
	Desc     string   // Human-readable description
	Required bool     // true = must be set, false = optional but recommended
	Alts     []string // Alternative keys (e.g. GITHUB_PAT for GITHUB_TOKEN)
}

// RequiredEnvVars returns the list of env vars that MUST be set for a given provider+auth config.
func RequiredEnvVars(pc *ProviderConfig) []EnvRequirement {
	// Common to all providers
	reqs := []EnvRequirement{
		{Key: "GITHUB_TOKEN", Desc: "GitHub access token for repo cloning", Required: false, Alts: []string{"GITHUB_PAT"}},
	}

	switch pc.Provider {
	case ProviderAnthropic:
		reqs = append(reqs,
			EnvRequirement{Key: "ANTHROPIC_API_KEY", Desc: "Anthropic API key", Required: true},
			EnvRequirement{Key: "ANTHROPIC_MODEL", Desc: "Model ID", Required: true},
		)

	case ProviderBedrock:
		reqs = append(reqs,
			EnvRequirement{Key: "CLAUDE_CODE_USE_BEDROCK", Desc: "Enable Bedrock mode (must be '1')", Required: true},
			EnvRequirement{Key: "AWS_REGION", Desc: "AWS region for Bedrock", Required: true},
			EnvRequirement{Key: "ANTHROPIC_MODEL", Desc: "Bedrock model ID", Required: true},
		)

		switch pc.BedrockAuth {
		case BedrockAuthLongTermKeys:
			reqs = append(reqs,
				EnvRequirement{Key: "AWS_ACCESS_KEY_ID", Desc: "AWS access key", Required: true},
				EnvRequirement{Key: "AWS_SECRET_ACCESS_KEY", Desc: "AWS secret key", Required: true},
			)
		case BedrockAuthSSO, BedrockAuthProfile:
			reqs = append(reqs,
				EnvRequirement{Key: "AWS_PROFILE", Desc: "AWS profile name", Required: true},
			)
		case BedrockAuthIAMRole, "":
			// No extra keys — inherited from instance/task role
		}

	case ProviderLiteLLM:
		reqs = append(reqs,
			EnvRequirement{Key: "ANTHROPIC_BASE_URL", Desc: "LiteLLM proxy URL", Required: true},
			EnvRequirement{Key: "ANTHROPIC_MODEL", Desc: "Model ID", Required: true},
			// ANTHROPIC_API_KEY optional for LiteLLM (may not require auth)
			EnvRequirement{Key: "ANTHROPIC_API_KEY", Desc: "LiteLLM proxy API key", Required: false},
		)
	}

	return reqs
}

// EnvValidationResult holds the result of environment validation.
type EnvValidationResult struct {
	Valid    bool             // true if all required vars are set
	Missing  []EnvRequirement // missing required env vars
	Warnings []string         // warnings about the environment
	Provider Provider         // provider being validated
	Auth     BedrockAuthMethod // Bedrock auth method (if applicable)
}

// ValidateWorkerEnv validates the current worker env against requirements.
func ValidateWorkerEnv(pc *ProviderConfig, currentEnv map[string]string) *EnvValidationResult {
	result := &EnvValidationResult{
		Valid:    true,
		Provider: pc.Provider,
		Auth:     pc.BedrockAuth,
	}

	reqs := RequiredEnvVars(pc)
	for _, req := range reqs {
		if !req.Required {
			continue
		}

		// Check if the key is set
		val, hasKey := currentEnv[req.Key]
		if !hasKey || val == "" {
			// Check alternatives
			found := false
			for _, alt := range req.Alts {
				if altVal, hasAlt := currentEnv[alt]; hasAlt && altVal != "" {
					found = true
					break
				}
			}
			if !found {
				result.Valid = false
				result.Missing = append(result.Missing, req)
			}
		}
	}

	// Provider-specific validations
	if pc.Provider == ProviderBedrock {
		// Check CLAUDE_CODE_USE_BEDROCK is exactly "1"
		if val, ok := currentEnv["CLAUDE_CODE_USE_BEDROCK"]; ok && val != "1" {
			result.Warnings = append(result.Warnings, "CLAUDE_CODE_USE_BEDROCK must be '1' not '"+val+"'")
			result.Valid = false
		}

		// Warn if model ID doesn't look like a Bedrock ID
		if model, ok := currentEnv["ANTHROPIC_MODEL"]; ok {
			if model != "" && !strings.Contains(model, "anthropic") && !strings.Contains(model, "claude") {
				result.Warnings = append(result.Warnings, "Model ID '"+model+"' doesn't look like a Bedrock model (should contain 'anthropic' or 'claude')")
			}
		}
	}

	return result
}
