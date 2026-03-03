package config

import "testing"

func TestResolveModelAliases(t *testing.T) {
	tests := []struct {
		alias    string
		provider Provider
		pins     map[string]string
		want     string
	}{
		// Anthropic aliases
		{"sonnet", ProviderAnthropic, nil, "claude-sonnet-4-6"},
		{"opus", ProviderAnthropic, nil, "claude-opus-4-6"},
		{"haiku", ProviderAnthropic, nil, "claude-haiku-4-5"},

		// Bedrock aliases
		{"sonnet", ProviderBedrock, nil, "us.anthropic.claude-sonnet-4-6"},
		{"opus", ProviderBedrock, nil, "us.anthropic.claude-opus-4-6-v1"},
		{"haiku", ProviderBedrock, nil, "us.anthropic.claude-haiku-4-5-20251001-v1:0"},

		// LiteLLM uses Anthropic IDs
		{"sonnet", ProviderLiteLLM, nil, "claude-sonnet-4-6"},
		{"opus", ProviderLiteLLM, nil, "claude-opus-4-6"},

		// Raw model ID (not an alias) passes through unchanged
		{"us.anthropic.claude-opus-4-6-v1", ProviderAnthropic, nil, "us.anthropic.claude-opus-4-6-v1"},
		{"custom-model-id", ProviderBedrock, nil, "custom-model-id"},

		// Pinned model overrides alias
		{"sonnet", ProviderBedrock, map[string]string{"sonnet": "us.anthropic.claude-sonnet-4-20250514-v1:0"}, "us.anthropic.claude-sonnet-4-20250514-v1:0"},
		{"opus", ProviderAnthropic, map[string]string{"opus": "claude-opus-4-20250514"}, "claude-opus-4-20250514"},
	}

	for _, tt := range tests {
		got := ResolveModel(tt.alias, tt.provider, tt.pins)
		if got != tt.want {
			t.Errorf("ResolveModel(%q, %q, %v) = %q; want %q", tt.alias, tt.provider, tt.pins, got, tt.want)
		}
	}
}

func TestWorkerEnvVars(t *testing.T) {
	t.Run("Bedrock", func(t *testing.T) {
		pc := &ProviderConfig{
			Provider:  ProviderBedrock,
			AWSRegion: "us-west-2",
		}
		vars := WorkerEnvVars(pc, "opus")
		if vars["CLAUDE_CODE_USE_BEDROCK"] != "1" {
			t.Error("Expected CLAUDE_CODE_USE_BEDROCK=1")
		}
		if vars["AWS_REGION"] != "us-west-2" {
			t.Errorf("Expected AWS_REGION=us-west-2, got %s", vars["AWS_REGION"])
		}
		if vars["ANTHROPIC_MODEL"] != "us.anthropic.claude-opus-4-6-v1" {
			t.Errorf("Expected Bedrock opus model, got %s", vars["ANTHROPIC_MODEL"])
		}
	})

	t.Run("LiteLLM", func(t *testing.T) {
		pc := &ProviderConfig{
			Provider: ProviderLiteLLM,
			ProxyURL: "https://proxy.example.com",
			ProxyKey: "sk-proxy-123",
		}
		vars := WorkerEnvVars(pc, "sonnet")
		if vars["ANTHROPIC_BASE_URL"] != "https://proxy.example.com" {
			t.Errorf("Expected proxy URL, got %s", vars["ANTHROPIC_BASE_URL"])
		}
		if vars["ANTHROPIC_API_KEY"] != "sk-proxy-123" {
			t.Errorf("Expected proxy key, got %s", vars["ANTHROPIC_API_KEY"])
		}
		if vars["ANTHROPIC_MODEL"] != "claude-sonnet-4-6" {
			t.Errorf("Expected Anthropic-style model, got %s", vars["ANTHROPIC_MODEL"])
		}
	})

	t.Run("Anthropic", func(t *testing.T) {
		pc := &ProviderConfig{Provider: ProviderAnthropic}
		vars := WorkerEnvVars(pc, "haiku")
		if _, ok := vars["CLAUDE_CODE_USE_BEDROCK"]; ok {
			t.Error("Should not set CLAUDE_CODE_USE_BEDROCK for Anthropic")
		}
		if vars["ANTHROPIC_MODEL"] != "claude-haiku-4-5" {
			t.Errorf("Expected haiku model, got %s", vars["ANTHROPIC_MODEL"])
		}
	})

	t.Run("Bedrock with pins", func(t *testing.T) {
		pc := &ProviderConfig{
			Provider:  ProviderBedrock,
			AWSRegion: "us-east-1",
			ModelPins: map[string]string{
				"opus":   "us.anthropic.claude-opus-4-6-v1",
				"sonnet": "us.anthropic.claude-sonnet-4-6",
				"haiku":  "us.anthropic.claude-haiku-4-5-20251001-v1:0",
			},
		}
		vars := WorkerEnvVars(pc, "opus")
		if vars["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "us.anthropic.claude-opus-4-6-v1" {
			t.Error("Missing pinned opus model")
		}
		if vars["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "us.anthropic.claude-sonnet-4-6" {
			t.Error("Missing pinned sonnet model")
		}
	})
}

func TestIsValidProvider(t *testing.T) {
	if !IsValidProvider("anthropic") { t.Error("anthropic should be valid") }
	if !IsValidProvider("bedrock") { t.Error("bedrock should be valid") }
	if !IsValidProvider("litellm") { t.Error("litellm should be valid") }
	if IsValidProvider("openai") { t.Error("openai should be invalid") }
	if IsValidProvider("") { t.Error("empty should be invalid") }
}

func TestRequiredEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		config   *ProviderConfig
		expected []string // expected required env var keys
	}{
		{
			name: "Anthropic provider",
			config: &ProviderConfig{
				Provider: ProviderAnthropic,
			},
			expected: []string{"ANTHROPIC_API_KEY", "ANTHROPIC_MODEL"},
		},
		{
			name: "Bedrock with IAM role",
			config: &ProviderConfig{
				Provider:    ProviderBedrock,
				BedrockAuth: BedrockAuthIAMRole,
			},
			expected: []string{"CLAUDE_CODE_USE_BEDROCK", "AWS_REGION", "ANTHROPIC_MODEL"},
		},
		{
			name: "Bedrock with long-term keys",
			config: &ProviderConfig{
				Provider:    ProviderBedrock,
				BedrockAuth: BedrockAuthLongTermKeys,
			},
			expected: []string{"CLAUDE_CODE_USE_BEDROCK", "AWS_REGION", "ANTHROPIC_MODEL",
				"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"},
		},
		{
			name: "Bedrock with profile",
			config: &ProviderConfig{
				Provider:    ProviderBedrock,
				BedrockAuth: BedrockAuthProfile,
			},
			expected: []string{"CLAUDE_CODE_USE_BEDROCK", "AWS_REGION", "ANTHROPIC_MODEL", "AWS_PROFILE"},
		},
		{
			name: "Bedrock with SSO",
			config: &ProviderConfig{
				Provider:    ProviderBedrock,
				BedrockAuth: BedrockAuthSSO,
			},
			expected: []string{"CLAUDE_CODE_USE_BEDROCK", "AWS_REGION", "ANTHROPIC_MODEL", "AWS_PROFILE"},
		},
		{
			name: "LiteLLM provider",
			config: &ProviderConfig{
				Provider: ProviderLiteLLM,
			},
			expected: []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqs := RequiredEnvVars(tt.config)

			// Extract required keys
			var requiredKeys []string
			for _, req := range reqs {
				if req.Required {
					requiredKeys = append(requiredKeys, req.Key)
				}
			}

			// Check all expected keys are present
			for _, expectedKey := range tt.expected {
				found := false
				for _, key := range requiredKeys {
					if key == expectedKey {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected required key %s not found", expectedKey)
				}
			}

			// Check no unexpected required keys
			for _, key := range requiredKeys {
				found := false
				for _, expectedKey := range tt.expected {
					if key == expectedKey {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Unexpected required key %s", key)
				}
			}
		})
	}
}

func TestValidateWorkerEnv(t *testing.T) {
	tests := []struct {
		name        string
		config      *ProviderConfig
		currentEnv  map[string]string
		expectValid bool
		expectMissing []string
	}{
		{
			name: "Anthropic all vars set",
			config: &ProviderConfig{
				Provider: ProviderAnthropic,
			},
			currentEnv: map[string]string{
				"ANTHROPIC_API_KEY": "test-key",
				"ANTHROPIC_MODEL":   "claude-sonnet-4-6",
			},
			expectValid: true,
		},
		{
			name: "Anthropic missing API key",
			config: &ProviderConfig{
				Provider: ProviderAnthropic,
			},
			currentEnv: map[string]string{
				"ANTHROPIC_MODEL": "claude-sonnet-4-6",
			},
			expectValid:   false,
			expectMissing: []string{"ANTHROPIC_API_KEY"},
		},
		{
			name: "Bedrock IAM role all vars set",
			config: &ProviderConfig{
				Provider:    ProviderBedrock,
				BedrockAuth: BedrockAuthIAMRole,
			},
			currentEnv: map[string]string{
				"CLAUDE_CODE_USE_BEDROCK": "1",
				"AWS_REGION":              "us-east-1",
				"ANTHROPIC_MODEL":         "us.anthropic.claude-sonnet-4-6",
			},
			expectValid: true,
		},
		{
			name: "Bedrock wrong CLAUDE_CODE_USE_BEDROCK value",
			config: &ProviderConfig{
				Provider:    ProviderBedrock,
				BedrockAuth: BedrockAuthIAMRole,
			},
			currentEnv: map[string]string{
				"CLAUDE_CODE_USE_BEDROCK": "true", // Should be "1"
				"AWS_REGION":              "us-east-1",
				"ANTHROPIC_MODEL":         "us.anthropic.claude-sonnet-4-6",
			},
			expectValid: false,
		},
		{
			name: "Bedrock long-term keys all set",
			config: &ProviderConfig{
				Provider:    ProviderBedrock,
				BedrockAuth: BedrockAuthLongTermKeys,
			},
			currentEnv: map[string]string{
				"CLAUDE_CODE_USE_BEDROCK": "1",
				"AWS_REGION":              "us-east-1",
				"ANTHROPIC_MODEL":         "us.anthropic.claude-sonnet-4-6",
				"AWS_ACCESS_KEY_ID":       "AKIAIOSFODNN7EXAMPLE",
				"AWS_SECRET_ACCESS_KEY":   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			},
			expectValid: true,
		},
		{
			name: "Bedrock long-term keys missing secret",
			config: &ProviderConfig{
				Provider:    ProviderBedrock,
				BedrockAuth: BedrockAuthLongTermKeys,
			},
			currentEnv: map[string]string{
				"CLAUDE_CODE_USE_BEDROCK": "1",
				"AWS_REGION":              "us-east-1",
				"ANTHROPIC_MODEL":         "us.anthropic.claude-sonnet-4-6",
				"AWS_ACCESS_KEY_ID":       "AKIAIOSFODNN7EXAMPLE",
			},
			expectValid:   false,
			expectMissing: []string{"AWS_SECRET_ACCESS_KEY"},
		},
		{
			name: "LiteLLM with all required vars",
			config: &ProviderConfig{
				Provider: ProviderLiteLLM,
			},
			currentEnv: map[string]string{
				"ANTHROPIC_BASE_URL": "http://localhost:4000",
				"ANTHROPIC_MODEL":    "claude-sonnet-4-6",
			},
			expectValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateWorkerEnv(tt.config, tt.currentEnv)

			if result.Valid != tt.expectValid {
				t.Errorf("Expected Valid=%v, got %v", tt.expectValid, result.Valid)
			}

			// Check missing vars
			if len(tt.expectMissing) > 0 {
				missingKeys := make(map[string]bool)
				for _, req := range result.Missing {
					missingKeys[req.Key] = true
				}

				for _, expectedKey := range tt.expectMissing {
					if !missingKeys[expectedKey] {
						t.Errorf("Expected %s to be missing", expectedKey)
					}
				}
			}
		})
	}
}

func TestWorkerEnvVarsWithAuth(t *testing.T) {
	tests := []struct {
		name     string
		config   *ProviderConfig
		model    string
		expected map[string]string
		notExpected []string
	}{
		{
			name: "Bedrock with IAM role",
			config: &ProviderConfig{
				Provider:    ProviderBedrock,
				AWSRegion:   "us-west-2",
				BedrockAuth: BedrockAuthIAMRole,
			},
			model: "opus",
			expected: map[string]string{
				"CLAUDE_CODE_USE_BEDROCK":   "1",
				"AWS_REGION":                "us-west-2",
				"ANTHROPIC_MODEL":           "us.anthropic.claude-opus-4-6-v1",
			},
			notExpected: []string{"AWS_PROFILE", "AWS_ACCESS_KEY_ID"},
		},
		{
			name: "Bedrock with profile auth",
			config: &ProviderConfig{
				Provider:    ProviderBedrock,
				AWSRegion:   "us-east-1",
				BedrockAuth: BedrockAuthProfile,
				AWSProfile:  "my-profile",
			},
			model: "sonnet",
			expected: map[string]string{
				"CLAUDE_CODE_USE_BEDROCK": "1",
				"AWS_REGION":              "us-east-1",
				"AWS_PROFILE":             "my-profile",
				"ANTHROPIC_MODEL":         "us.anthropic.claude-sonnet-4-6",
			},
			notExpected: []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"},
		},
		{
			name: "Bedrock with SSO",
			config: &ProviderConfig{
				Provider:    ProviderBedrock,
				BedrockAuth: BedrockAuthSSO,
				AWSProfile:  "sso-profile",
			},
			model: "sonnet",
			expected: map[string]string{
				"CLAUDE_CODE_USE_BEDROCK": "1",
				"AWS_REGION":              "us-east-1", // default
				"AWS_PROFILE":             "sso-profile",
				"ANTHROPIC_MODEL":         "us.anthropic.claude-sonnet-4-6",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := WorkerEnvVars(tt.config, tt.model)

			// Check all expected vars are present with correct values
			for key, expectedVal := range tt.expected {
				if val, ok := result[key]; !ok {
					t.Errorf("Missing expected env var %s", key)
				} else if val != expectedVal {
					t.Errorf("For %s: expected %s, got %s", key, expectedVal, val)
				}
			}

			// Check vars that should NOT be present
			for _, key := range tt.notExpected {
				if _, ok := result[key]; ok {
					t.Errorf("Unexpected env var %s present", key)
				}
			}
		})
	}
}
