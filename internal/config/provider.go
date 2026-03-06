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
	BedrockAuthIAMRole  BedrockAuthMethod = "iam-role"   // EC2 instance profile / ECS task role
	BedrockAuthAPIKeys  BedrockAuthMethod = "api-keys"   // AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY
	BedrockAuthSSO      BedrockAuthMethod = "sso"        // AWS SSO / Identity Center
	BedrockAuthProfile  BedrockAuthMethod = "profile"    // Named AWS profile (~/.aws/credentials)
)

// ValidProviders returns all supported provider names from the bundle.
func ValidProviders() []string {
	pf, err := LoadProviders()
	if err != nil {
		// Fallback to hardcoded if bundle loading fails
		return []string{"anthropic", "bedrock", "litellm"}
	}
	names := make([]string, 0, len(pf.Providers))
	for k := range pf.Providers {
		names = append(names, k)
	}
	return names
}

// IsValidProvider returns true if the provider name is known.
func IsValidProvider(p string) bool {
	pf, err := LoadProviders()
	if err != nil {
		switch Provider(p) {
		case ProviderAnthropic, ProviderBedrock, ProviderLiteLLM:
			return true
		}
		return false
	}
	_, ok := pf.Providers[p]
	return ok
}

// ProviderConfig holds provider-specific configuration.
type ProviderConfig struct {
	Provider     Provider          `json:"provider,omitempty"`
	AWSRegion    string            `json:"awsRegion,omitempty"`
	BedrockAuth  BedrockAuthMethod `json:"bedrockAuth,omitempty"`
	AWSProfile   string            `json:"awsProfile,omitempty"`
	ProxyURL     string            `json:"proxyUrl,omitempty"`
	ProxyKey     string            `json:"proxyKey,omitempty"`
	SmallModel   string            `json:"smallModel,omitempty"`
	ModelPins    map[string]string `json:"modelPins,omitempty"`
}

// ModelAlias maps human-friendly names to provider-specific model IDs.
type ModelAlias struct {
	Alias     string
	Anthropic string
	Bedrock   string
}

// KnownAliases returns the standard model alias table from the bundle.
func KnownAliases() []ModelAlias {
	pf, err := LoadProviders()
	if err != nil {
		return nil
	}

	// Build aliases from the intersection of anthropic and bedrock models
	anthro, hasAnthro := pf.Providers["anthropic"]
	bedrock, hasBedrock := pf.Providers["bedrock"]

	if !hasAnthro {
		return nil
	}

	var aliases []ModelAlias
	for alias, anthroModel := range anthro.Models {
		bedrockModel := ""
		if hasBedrock {
			bedrockModel = bedrock.Models[alias]
		}
		aliases = append(aliases, ModelAlias{
			Alias:     alias,
			Anthropic: anthroModel,
			Bedrock:   bedrockModel,
		})
	}
	return aliases
}

// ResolveModel takes an alias or raw model ID and returns the provider-specific model ID.
func ResolveModel(alias string, provider Provider, pins map[string]string) string {
	// Check pins first
	if pins != nil {
		if pinned, ok := pins[alias]; ok {
			return pinned
		}
	}

	// Check bundle
	b, err := GetProviderBundle(provider)
	if err == nil {
		if modelID, ok := b.Models[alias]; ok {
			return modelID
		}
	}

	// Not an alias — return as-is
	return alias
}

// DefaultSmallModel returns the default small/fast model from the bundle.
func DefaultSmallModel(provider Provider) string {
	b, err := GetProviderBundle(provider)
	if err != nil {
		return "claude-haiku-4-5"
	}
	if b.SmallMod != "" {
		return b.SmallMod
	}
	return "claude-haiku-4-5"
}

// WorkerEnvVars returns the env vars the worker needs for a given provider config.
// Reads from providers.json for fixed values, applies config-specific overrides.
func WorkerEnvVars(pc *ProviderConfig, model string) map[string]string {
	vars := map[string]string{}

	resolved := ResolveModel(model, pc.Provider, pc.ModelPins)
	smallResolved := pc.SmallModel
	if smallResolved == "" {
		smallResolved = DefaultSmallModel(pc.Provider)
	}

	b, err := GetProviderBundle(pc.Provider)
	if err != nil {
		// Fallback: minimal vars
		vars["ANTHROPIC_MODEL"] = resolved
		return vars
	}

	// Set fixed-value env vars from bundle
	for _, ev := range b.EnvVars.Always {
		if ev.Value != "" {
			vars[ev.Key] = ev.Value
		}
	}

	// Set model vars
	vars["ANTHROPIC_MODEL"] = resolved
	if smallResolved != "" {
		vars["ANTHROPIC_SMALL_FAST_MODEL"] = smallResolved
	}

	// Provider-specific overrides
	switch pc.Provider {
	case ProviderBedrock:
		region := pc.AWSRegion
		if region == "" {
			region = "us-east-1"
		}
		vars["AWS_REGION"] = region

		// Auth method specific
		if b.EnvVars.AuthMethods != nil {
			authKey := string(pc.BedrockAuth)
			if authKey == "" {
				authKey = b.EnvVars.DefaultAuthMethod
			}
			if am, ok := b.EnvVars.AuthMethods[authKey]; ok {
				for _, ev := range am.EnvVars {
					if ev.Value != "" {
						vars[ev.Key] = ev.Value
					}
				}
			}
		}

		// Profile-based auth
		if (pc.BedrockAuth == BedrockAuthSSO || pc.BedrockAuth == BedrockAuthProfile) && pc.AWSProfile != "" {
			vars["AWS_PROFILE"] = pc.AWSProfile
		}

		// Version pins
		if pc.ModelPins != nil && b.PinVars != nil {
			for alias, envVar := range b.PinVars {
				if pinned, ok := pc.ModelPins[alias]; ok {
					vars[envVar] = pinned
				}
			}
		}

	case ProviderLiteLLM:
		if pc.ProxyKey != "" {
			vars["ANTHROPIC_API_KEY"] = pc.ProxyKey
		}
		if pc.ProxyURL != "" {
			vars["ANTHROPIC_BASE_URL"] = pc.ProxyURL
		}
	}

	return vars
}

// EnvRequirement describes an environment variable requirement.
type EnvRequirement struct {
	Key      string
	Desc     string
	Required bool
	Alts     []string
}

// RequiredEnvVars returns the list of env vars that must/should be set, driven by providers.json.
func RequiredEnvVars(pc *ProviderConfig) []EnvRequirement {
	return RequiredEnvVarsWithGit(pc, "")
}

// RequiredEnvVarsWithGit returns env var requirements including git provider.
func RequiredEnvVarsWithGit(pc *ProviderConfig, gitProvider string) []EnvRequirement {
	pf, err := LoadProviders()
	if err != nil {
		return nil
	}

	// Start with common
	var reqs []EnvRequirement
	for _, ev := range pf.CommonEnvVars {
		reqs = append(reqs, EnvRequirement{Key: ev.Key, Desc: ev.Desc, Required: ev.Required, Alts: ev.Alts})
	}

	b, ok := pf.Providers[string(pc.Provider)]
	if !ok {
		return reqs
	}

	// Always-required env vars
	for _, ev := range b.EnvVars.Always {
		reqs = append(reqs, EnvRequirement{Key: ev.Key, Desc: ev.Desc, Required: ev.Required, Alts: ev.Alts})
	}

	// Auth-method-specific env vars (Bedrock)
	if b.EnvVars.AuthMethods != nil {
		authKey := string(pc.BedrockAuth)
		if authKey == "" {
			authKey = b.EnvVars.DefaultAuthMethod
		}
		if am, ok := b.EnvVars.AuthMethods[authKey]; ok {
			for _, ev := range am.EnvVars {
				reqs = append(reqs, EnvRequirement{Key: ev.Key, Desc: ev.Desc, Required: ev.Required, Alts: ev.Alts})
			}
		}
	}

	// Git provider env vars
	if gitProvider != "" {
		gitReqs := GitProviderEnvVars(gitProvider)
		reqs = append(reqs, gitReqs...)
	}

	return reqs
}

// EnvValidationResult holds the result of environment validation.
type EnvValidationResult struct {
	Valid    bool
	Missing  []EnvRequirement
	Warnings []string
	Provider Provider
	Auth     BedrockAuthMethod
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

		val, hasKey := currentEnv[req.Key]
		if !hasKey || val == "" {
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
		if val, ok := currentEnv["CLAUDE_CODE_USE_BEDROCK"]; ok && val != "1" {
			result.Warnings = append(result.Warnings, "CLAUDE_CODE_USE_BEDROCK must be '1' not '"+val+"'")
			result.Valid = false
		}

		if model, ok := currentEnv["ANTHROPIC_MODEL"]; ok {
			if model != "" && !strings.Contains(model, "anthropic") && !strings.Contains(model, "claude") {
				result.Warnings = append(result.Warnings, "Model ID '"+model+"' doesn't look like a Bedrock model (should contain 'anthropic' or 'claude')")
			}
		}
	}

	return result
}
