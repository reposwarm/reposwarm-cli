package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newConfigProviderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "Configure LLM provider (Anthropic, Bedrock, LiteLLM)",
	}
	cmd.AddCommand(newProviderSetupCmd())
	cmd.AddCommand(newProviderSetCmd())
	cmd.AddCommand(newProviderShowCmd())
	return cmd
}

func newProviderSetupCmd() *cobra.Command {
	var (
		providerFlag   string
		regionFlag     string
		modelFlag      string
		proxyURLFlag   string
		proxyKeyFlag   string
		pinFlag        bool
		nonInterFlag   bool
		authMethodFlag string
		awsProfileFlag string
		awsKeyFlag     string
		awsSecretFlag  string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive provider setup wizard",
		Long: `Set up the LLM provider for RepoSwarm investigations.

Supported providers:
  anthropic  — Direct Anthropic API (needs ANTHROPIC_API_KEY)
  bedrock    — Amazon Bedrock (needs AWS credentials)
  litellm    — LiteLLM proxy (needs proxy URL and optional key)

Interactive mode walks you through each step.
Non-interactive mode requires --provider and provider-specific flags.

Examples:
  reposwarm config provider setup
  reposwarm config provider setup --provider bedrock --region us-east-1 --model opus --pin
  reposwarm config provider setup --provider litellm --proxy-url https://my-proxy.example.com --model sonnet`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			provider := providerFlag
			region := regionFlag
			model := modelFlag
			proxyURL := proxyURLFlag
			proxyKey := proxyKeyFlag
			pin := pinFlag
			authMethod := authMethodFlag
			awsProfile := awsProfileFlag
			awsKey := awsKeyFlag
			awsSecret := awsSecretFlag

			if !nonInterFlag && provider == "" {
				// Interactive mode
				reader := bufio.NewReader(os.Stdin)

				fmt.Println()
				output.F.Section("Provider Setup")
				fmt.Println()
				fmt.Println("  Which LLM provider should RepoSwarm use?")
				fmt.Println()
				fmt.Println("  1) anthropic  — Direct Anthropic API (API key)")
				fmt.Println("  2) bedrock    — Amazon Bedrock (AWS credentials)")
				fmt.Println("  3) litellm    — LiteLLM proxy (custom endpoint)")
				fmt.Println()

				provider = promptChoice(reader, "Provider [1/2/3]", map[string]string{
					"1": "anthropic", "2": "bedrock", "3": "litellm",
					"anthropic": "anthropic", "bedrock": "bedrock", "litellm": "litellm",
				}, "anthropic")

				switch config.Provider(provider) {
				case config.ProviderBedrock:
					if region == "" {
						region = promptString(reader, "AWS Region", "us-east-1")
					}

					// Ask for auth method
					if authMethod == "" {
						fmt.Println()
						fmt.Println("  How does the worker authenticate with AWS?")
						fmt.Println()
						fmt.Println("  1) iam-role        — EC2 instance profile or ECS task role (recommended)")
						fmt.Println("  2) long-term-keys  — AWS access key + secret (Bedrock API keys)")
						fmt.Println("  3) profile         — Named AWS profile (~/.aws/credentials or SSO)")
						fmt.Println()

						authMethod = promptChoice(reader, "Auth method [1/2/3]", map[string]string{
							"1": "iam-role", "2": "long-term-keys", "3": "profile",
							"iam-role": "iam-role", "long-term-keys": "long-term-keys", "profile": "profile",
						}, "iam-role")
					}

					// Prompt for auth-specific info
					switch config.BedrockAuthMethod(authMethod) {
					case config.BedrockAuthLongTermKeys:
						if awsKey == "" {
							awsKey = promptString(reader, "AWS Access Key ID", "")
						}
						if awsSecret == "" {
							awsSecret = promptString(reader, "AWS Secret Access Key", "")
						}
					case config.BedrockAuthProfile, config.BedrockAuthSSO:
						if awsProfile == "" {
							awsProfile = promptString(reader, "AWS Profile name", "default")
						}
					}

				case config.ProviderLiteLLM:
					if proxyURL == "" {
						proxyURL = promptString(reader, "LiteLLM proxy URL", "http://localhost:4000")
					}
					if proxyKey == "" {
						proxyKey = promptString(reader, "LiteLLM proxy API key (blank if none)", "")
					}
				}

				if model == "" {
					fmt.Println()
					fmt.Println("  Model aliases: sonnet, opus, haiku")
					fmt.Println("  Or specify a full model ID.")
					model = promptString(reader, "Model", "sonnet")
				}

				if !pin {
					fmt.Println()
					pinStr := promptString(reader, "Pin model versions? (recommended for stability) [y/N]", "n")
					pin = strings.ToLower(pinStr) == "y" || strings.ToLower(pinStr) == "yes"
				}
			}

			if provider == "" {
				return fmt.Errorf("--provider is required in non-interactive mode")
			}
			if !config.IsValidProvider(provider) {
				return fmt.Errorf("unknown provider: %s (valid: %s)", provider, strings.Join(config.ValidProviders(), ", "))
			}

			// Apply to config
			cfg.ProviderConfig.Provider = config.Provider(provider)

			switch config.Provider(provider) {
			case config.ProviderBedrock:
				if region == "" {
					region = "us-east-1"
				}
				cfg.ProviderConfig.AWSRegion = region

				// Set auth method
				if authMethod == "" {
					authMethod = "iam-role" // default
				}
				cfg.ProviderConfig.BedrockAuth = config.BedrockAuthMethod(authMethod)

				// Set profile if using profile auth
				if config.BedrockAuthMethod(authMethod) == config.BedrockAuthProfile ||
				   config.BedrockAuthMethod(authMethod) == config.BedrockAuthSSO {
					cfg.ProviderConfig.AWSProfile = awsProfile
				}

			case config.ProviderLiteLLM:
				cfg.ProviderConfig.ProxyURL = proxyURL
				cfg.ProviderConfig.ProxyKey = proxyKey
			}

			// Resolve model
			if model == "" {
				model = "sonnet"
			}
			resolved := config.ResolveModel(model, config.Provider(provider), nil)
			cfg.DefaultModel = resolved

			// Clear old pins from different provider, then optionally re-pin
			if !pin {
				cfg.ProviderConfig.ModelPins = nil
			}
			if pin {
				cfg.ProviderConfig.ModelPins = map[string]string{}
				for _, a := range config.KnownAliases() {
					switch config.Provider(provider) {
					case config.ProviderBedrock:
						cfg.ProviderConfig.ModelPins[a.Alias] = a.Bedrock
					default:
						cfg.ProviderConfig.ModelPins[a.Alias] = a.Anthropic
					}
				}
			}

			// Push env vars to worker via API (before saving config, for inference check)
			workerVars := config.WorkerEnvVars(&cfg.ProviderConfig, model)

			// Add AWS credentials for long-term-keys auth
			if config.Provider(provider) == config.ProviderBedrock &&
			   config.BedrockAuthMethod(authMethod) == config.BedrockAuthLongTermKeys {
				if awsKey != "" {
					workerVars["AWS_ACCESS_KEY_ID"] = awsKey
				}
				if awsSecret != "" {
					workerVars["AWS_SECRET_ACCESS_KEY"] = awsSecret
				}
			}

			client, clientErr := getClient()
			inferenceOK := false

			if clientErr == nil {
				// Sync env vars first (needed for inference check)
				for k, v := range workerVars {
					body := map[string]string{"value": v}
					var resp any
					if err := client.Put(ctx(), "/workers/worker-1/env/"+k, body, &resp); err != nil {
						if !flagJSON {
							output.F.Warning(fmt.Sprintf("Could not set worker env %s: %v", k, err))
						}
					}
				}

				// Run inference health check BEFORE saving config
				if !flagJSON {
					fmt.Println()
					output.F.Section("Inference Health Check")
					fmt.Print("  Testing model connection... ")
				}

				var inferenceResp struct {
					Success     bool   `json:"success"`
					Provider    string `json:"provider"`
					Model       string `json:"model"`
					AuthMethod  string `json:"authMethod"`
					LatencyMs   int    `json:"latencyMs"`
					Response    string `json:"response"`
					Error       string `json:"error"`
					Hint        string `json:"hint"`
				}

				if err := client.Post(ctx(), "/workers/worker-1/inference-check", nil, &inferenceResp); err != nil {
					if !flagJSON {
						output.F.Println()
						output.F.Warning(fmt.Sprintf("Could not run inference check: %v", err))
						output.F.Info("The inference-check endpoint may not be available on this API server version.")
						output.F.Info("Saving config anyway — verify manually with: reposwarm doctor")
					}
				} else if inferenceResp.Success {
					inferenceOK = true
					if !flagJSON {
						output.Successf("✓ inference working (%dms)", inferenceResp.LatencyMs)
					}
				} else {
					if !flagJSON {
						output.F.Println()
						output.F.Error(fmt.Sprintf("✗ inference FAILED: %s", inferenceResp.Error))
						if inferenceResp.Hint != "" {
							output.F.Info(fmt.Sprintf("Hint: %s", inferenceResp.Hint))
						}
						fmt.Println()
						output.F.Warning("⚠ The model could not be reached with these settings.")
						output.F.Info("Possible fixes:")
						switch config.BedrockAuthMethod(authMethod) {
						case config.BedrockAuthIAMRole:
							output.F.Info("  • Check IAM role has bedrock:InvokeModel permission")
							output.F.Info("  • Verify the model is enabled in your AWS account/region")
							output.F.Info("  • Try: aws bedrock list-inference-profiles --region " + region)
						case config.BedrockAuthLongTermKeys:
							output.F.Info("  • Check AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are correct")
							output.F.Info("  • Verify the IAM user has bedrock:InvokeModel permission")
						case config.BedrockAuthSSO, config.BedrockAuthProfile:
							output.F.Info("  • Run: aws sso login --profile=" + cfg.ProviderConfig.AWSProfile)
							output.F.Info("  • Check profile has bedrock:InvokeModel permission")
						}
						if config.Provider(provider) == config.ProviderAnthropic {
							output.F.Info("  • Check ANTHROPIC_API_KEY is valid")
							output.F.Info("  • Verify API key has access to the model")
						}
						output.F.Info("Saving config anyway — fix the issue and run: reposwarm doctor")
					}
				}
			}

			// Save config (even if inference failed — user can fix later)
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			if flagJSON {
				return output.JSON(map[string]any{
					"provider":       provider,
					"model":          resolved,
					"region":         region,
					"proxyUrl":       proxyURL,
					"pinned":         pin,
					"workerVars":     workerVars,
					"inferenceCheck": inferenceOK,
				})
			}

			fmt.Println()
			output.Successf("Provider configured: %s", provider)
			output.F.KeyValue("Model", resolved)
			if region != "" {
				output.F.KeyValue("Region", region)
			}
			if proxyURL != "" {
				output.F.KeyValue("Proxy URL", proxyURL)
			}
			if pin {
				output.F.KeyValue("Pinned", "yes")
			}
			fmt.Println()

			if clientErr == nil {
				output.Successf("Worker env vars synced (%d vars)", len(workerVars))
				output.F.Warning("Worker restart required: reposwarm restart worker")
			} else {
				output.F.Warning("Could not sync to worker API — set env vars manually")
				fmt.Println()
				for k, v := range workerVars {
					fmt.Printf("  %s=%s\n", k, v)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&providerFlag, "provider", "", "Provider (anthropic|bedrock|litellm)")
	cmd.Flags().StringVar(&regionFlag, "region", "", "AWS region (Bedrock)")
	cmd.Flags().StringVar(&modelFlag, "model", "", "Model alias or ID")
	cmd.Flags().StringVar(&proxyURLFlag, "proxy-url", "", "LiteLLM proxy URL")
	cmd.Flags().StringVar(&proxyKeyFlag, "proxy-key", "", "LiteLLM proxy API key")
	cmd.Flags().BoolVar(&pinFlag, "pin", false, "Pin model versions")
	cmd.Flags().BoolVar(&nonInterFlag, "non-interactive", false, "Skip prompts")
	cmd.Flags().StringVar(&authMethodFlag, "auth-method", "", "Bedrock auth method (iam-role|long-term-keys|profile)")
	cmd.Flags().StringVar(&awsProfileFlag, "aws-profile", "", "AWS profile name (for profile auth)")
	cmd.Flags().StringVar(&awsKeyFlag, "aws-key", "", "AWS access key ID (for long-term-keys auth)")
	cmd.Flags().StringVar(&awsSecretFlag, "aws-secret", "", "AWS secret access key (for long-term-keys auth)")
	return cmd
}

func newProviderSetCmd() *cobra.Command {
	var checkFlag bool

	cmd := &cobra.Command{
		Use:   "set <provider>",
		Short: "Quick-switch provider (preserves other settings)",
		Args:  friendlyExactArgs(1, "reposwarm config provider set <provider>\n\nProviders: anthropic, bedrock, litellm\n\nExample:\n  reposwarm config provider set bedrock"),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			if !config.IsValidProvider(provider) {
				return fmt.Errorf("unknown provider: %s (valid: %s)", provider, strings.Join(config.ValidProviders(), ", "))
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			oldProvider := cfg.EffectiveProvider()
			cfg.ProviderConfig.Provider = config.Provider(provider)

			// Re-resolve default model for new provider
			for _, a := range config.KnownAliases() {
				switch oldProvider {
				case config.ProviderBedrock:
					if cfg.DefaultModel == a.Bedrock {
						cfg.DefaultModel = config.ResolveModel(a.Alias, config.Provider(provider), cfg.ProviderConfig.ModelPins)
					}
				default:
					if cfg.DefaultModel == a.Anthropic {
						cfg.DefaultModel = config.ResolveModel(a.Alias, config.Provider(provider), cfg.ProviderConfig.ModelPins)
					}
				}
			}

			// Clear pins from old provider (they have wrong format)
			cfg.ProviderConfig.ModelPins = nil

			if err := config.Save(cfg); err != nil {
				return err
			}

			// Sync worker env
			workerVars := config.WorkerEnvVars(&cfg.ProviderConfig, cfg.DefaultModel)
			client, clientErr := getClient()
			if clientErr == nil {
				for k, v := range workerVars {
					body := map[string]string{"value": v}
					var resp any
					client.Put(ctx(), "/workers/worker-1/env/"+k, body, &resp)
				}

				// Run inference check if requested
				if checkFlag {
					if !flagJSON {
						fmt.Println()
						output.F.Section("Inference Health Check")
						fmt.Print("  Testing model connection... ")
					}

					var inferenceResp struct {
						Success     bool   `json:"success"`
						Provider    string `json:"provider"`
						Model       string `json:"model"`
						AuthMethod  string `json:"authMethod"`
						LatencyMs   int    `json:"latencyMs"`
						Response    string `json:"response"`
						Error       string `json:"error"`
						Hint        string `json:"hint"`
					}

					if err := client.Post(ctx(), "/workers/worker-1/inference-check", nil, &inferenceResp); err != nil {
						if !flagJSON {
							output.F.Println()
							output.F.Warning(fmt.Sprintf("Could not run inference check: %v", err))
						}
					} else {
						if flagJSON {
							return output.JSON(map[string]any{
								"provider":       provider,
								"model":          cfg.DefaultModel,
								"inferenceCheck": inferenceResp.Success,
								"latencyMs":      inferenceResp.LatencyMs,
							})
						} else {
							if inferenceResp.Success {
								output.Successf("✓ inference working (%dms)", inferenceResp.LatencyMs)
							} else {
								output.F.Println()
								output.F.Error(fmt.Sprintf("✗ inference failed: %s", inferenceResp.Error))
								if inferenceResp.Hint != "" {
									output.F.Info(fmt.Sprintf("Hint: %s", inferenceResp.Hint))
								}
							}
						}
					}
				}
			}

			if flagJSON && !checkFlag {
				return output.JSON(map[string]any{
					"provider": provider,
					"model":    cfg.DefaultModel,
				})
			}

			if !flagJSON {
				output.Successf("Switched to %s (model: %s)", provider, cfg.DefaultModel)
				if clientErr == nil {
					output.F.Warning("Restart worker to apply: reposwarm restart worker")
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&checkFlag, "check", false, "Run inference health check after switching")
	return cmd
}

func newProviderShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show current provider configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			pc := cfg.ProviderConfig
			provider := cfg.EffectiveProvider()
			model := cfg.EffectiveModel()

			// Check worker env via API for validation
			var workerProvider string
			client, clientErr := getClient()
			if clientErr == nil {
				var envResp struct {
					Entries []struct {
						Key   string `json:"key"`
						Value string `json:"value"`
						Set   bool   `json:"set"`
					} `json:"entries"`
				}
				if err := client.Get(ctx(), "/workers/worker-1/env?reveal=true", &envResp); err == nil {
					for _, e := range envResp.Entries {
						if e.Key == "CLAUDE_CODE_USE_BEDROCK" && e.Set && e.Value == "1" {
							workerProvider = "bedrock"
						}
						if e.Key == "ANTHROPIC_BASE_URL" && e.Set {
							workerProvider = "litellm"
						}
					}
					if workerProvider == "" {
						workerProvider = "anthropic"
					}
				}
			}

			if flagJSON {
				result := map[string]any{
					"provider":    string(provider),
					"model":       model,
					"awsRegion":   pc.AWSRegion,
					"proxyUrl":    pc.ProxyURL,
					"smallModel":  pc.SmallModel,
					"modelPins":   pc.ModelPins,
				}
				if provider == config.ProviderBedrock {
					bedrockAuth := pc.BedrockAuth
					if bedrockAuth == "" {
						bedrockAuth = config.BedrockAuthIAMRole
					}
					result["bedrockAuth"] = string(bedrockAuth)
					if pc.AWSProfile != "" {
						result["awsProfile"] = pc.AWSProfile
					}
				}
				if workerProvider != "" {
					result["workerProvider"] = workerProvider
				}
				return output.JSON(result)
			}

			F := output.F
			F.Section("Provider Configuration")
			F.KeyValue("Provider", string(provider))

			// Show auth method for Bedrock
			if provider == config.ProviderBedrock {
				authMethod := pc.BedrockAuth
				if authMethod == "" {
					authMethod = config.BedrockAuthIAMRole // default
				}
				F.KeyValue("Auth Method", string(authMethod))
			}

			F.KeyValue("Model", model)

			switch provider {
			case config.ProviderBedrock:
				F.KeyValue("AWS Region", orDefault(pc.AWSRegion, "us-east-1"))
				if pc.BedrockAuth == config.BedrockAuthProfile || pc.BedrockAuth == config.BedrockAuthSSO {
					F.KeyValue("AWS Profile", orDefault(pc.AWSProfile, "(not set)"))
				}
			case config.ProviderLiteLLM:
				F.KeyValue("Proxy URL", orDefault(pc.ProxyURL, "(not set)"))
				if pc.ProxyKey != "" {
					F.KeyValue("Proxy Key", config.MaskedToken(pc.ProxyKey))
				}
			}

			if pc.SmallModel != "" {
				F.KeyValue("Small Model", pc.SmallModel)
			}

			if len(pc.ModelPins) > 0 {
				F.Println()
				F.Section("Model Pins")
				for alias, id := range pc.ModelPins {
					F.KeyValue(alias, id)
				}
			}

			// Drift check
			if workerProvider != "" && workerProvider != string(provider) {
				F.Println()
				F.Warning(fmt.Sprintf("⚠ Config drift: CLI says '%s' but worker is running '%s'", provider, workerProvider))
				F.Info("Run: reposwarm restart worker")
			}

			// Environment validation
			if clientErr == nil {
				var envResp struct {
					Entries []struct {
						Key   string `json:"key"`
						Value string `json:"value"`
						Set   bool   `json:"set"`
					} `json:"entries"`
				}
				if err := client.Get(ctx(), "/workers/worker-1/env?reveal=true", &envResp); err == nil {
					// Build current env map
					currentEnv := make(map[string]string)
					for _, e := range envResp.Entries {
						if e.Set {
							currentEnv[e.Key] = e.Value
						}
					}

					// Validate environment
					validation := config.ValidateWorkerEnv(&pc, currentEnv)

					F.Println()
					F.Section("Environment Validation")

					// Show required env vars and their status
					reqs := config.RequiredEnvVars(&pc)
					for _, req := range reqs {
						if !req.Required {
							continue // Skip optional vars in display
						}

						val, hasKey := currentEnv[req.Key]
						// Check alternatives
						if !hasKey || val == "" {
							for _, alt := range req.Alts {
								if altVal, hasAlt := currentEnv[alt]; hasAlt && altVal != "" {
									hasKey = true
									val = altVal
									break
								}
							}
						}

						if hasKey && val != "" {
							// For sensitive values, mask them
							displayVal := val
							if strings.Contains(req.Key, "KEY") || strings.Contains(req.Key, "SECRET") || strings.Contains(req.Key, "TOKEN") {
								displayVal = config.MaskedToken(val)
							}
							// Special handling for CLAUDE_CODE_USE_BEDROCK
							if req.Key == "CLAUDE_CODE_USE_BEDROCK" {
								F.KeyValue(fmt.Sprintf("✓ %s", req.Key), displayVal)
							} else if req.Key == "AWS_REGION" || req.Key == "ANTHROPIC_MODEL" {
								F.KeyValue(fmt.Sprintf("✓ %s", req.Key), displayVal)
							} else {
								F.KeyValue(fmt.Sprintf("✓ %s", req.Key), displayVal)
							}
						} else {
							F.KeyValue(fmt.Sprintf("✗ %s", req.Key), fmt.Sprintf("NOT SET — %s", req.Desc))
						}
					}

					// Special case for IAM role auth - show inherited message
					if pc.Provider == config.ProviderBedrock &&
					   (pc.BedrockAuth == config.BedrockAuthIAMRole || pc.BedrockAuth == "") {
						// Check if we're NOT using long-term keys
						_, hasAccessKey := currentEnv["AWS_ACCESS_KEY_ID"]
						_, hasSecretKey := currentEnv["AWS_SECRET_ACCESS_KEY"]
						if !hasAccessKey && !hasSecretKey {
							F.KeyValue("✓ AWS credentials", "inherited (IAM role)")
						}
					}

					// Show warnings
					for _, warning := range validation.Warnings {
						F.Warning(warning)
					}

					// Overall status
					if !validation.Valid {
						F.Println()
						F.Error("Environment validation failed. Run 'reposwarm config provider setup' to fix.")
					}
				}
			}

			return nil
		},
	}
	return cmd
}

// ── Prompt helpers ──

func promptString(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("  %s: ", label)
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func promptChoice(reader *bufio.Reader, label string, choices map[string]string, defaultVal string) string {
	for {
		fmt.Printf("  %s: ", label)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			return defaultVal
		}
		if val, ok := choices[strings.ToLower(input)]; ok {
			return val
		}
		fmt.Printf("    Invalid choice. Try: %s\n", strings.Join(mapKeys(choices), ", "))
	}
}

func mapKeys(m map[string]string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, v := range m {
		if !seen[v] {
			keys = append(keys, v)
			seen[v] = true
		}
	}
	return keys
}

func orDefault(val, def string) string {
	if val == "" {
		return def
	}
	return val
}
