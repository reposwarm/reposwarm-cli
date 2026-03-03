# RepoSwarm: Provider-Aware Env Validation + Inference Health Check

## Overview
When a provider is set (via `config provider setup` or `config provider set`), the CLI should:
1. Set the correct env vars based on provider AND authentication method
2. Validate that all required env vars are actually set/available
3. Run an inference health check (via API server) to confirm the model actually works

## Part 1: CLI Changes (Go — /tmp/reposwarm-cli)

### 1A. Add `AuthMethod` to ProviderConfig (`internal/config/provider.go`)

Add a new `BedrockAuthMethod` type and field to `ProviderConfig`:

```go
type BedrockAuthMethod string

const (
    BedrockAuthIAMRole      BedrockAuthMethod = "iam-role"       // EC2 instance profile / ECS task role
    BedrockAuthLongTermKeys BedrockAuthMethod = "long-term-keys" // AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY
    BedrockAuthSSO          BedrockAuthMethod = "sso"            // AWS SSO / Identity Center
    BedrockAuthProfile      BedrockAuthMethod = "profile"        // Named AWS profile (~/.aws/credentials)
)
```

Add to `ProviderConfig`:
```go
type ProviderConfig struct {
    // ... existing fields ...
    BedrockAuth    BedrockAuthMethod `json:"bedrockAuth,omitempty"`
    AWSProfile     string            `json:"awsProfile,omitempty"`    // for "profile" auth method
}
```

### 1B. Update `WorkerEnvVars()` to be auth-method-aware

For Bedrock provider, the env vars differ by auth method:

**iam-role** (EC2/ECS inherited):
- `CLAUDE_CODE_USE_BEDROCK=1`
- `AWS_REGION=<region>`
- `ANTHROPIC_MODEL=<model>`
- Do NOT set AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY (inherited)

**long-term-keys**:
- `CLAUDE_CODE_USE_BEDROCK=1`
- `AWS_REGION=<region>`
- `AWS_ACCESS_KEY_ID=<key>` (note: value stored separately, not in config)
- `AWS_SECRET_ACCESS_KEY=<secret>` (note: value stored separately, not in config)
- `ANTHROPIC_MODEL=<model>`

**sso**:
- `CLAUDE_CODE_USE_BEDROCK=1`
- `AWS_REGION=<region>`
- `AWS_PROFILE=<profile>` (SSO profile name)
- `ANTHROPIC_MODEL=<model>`

**profile**:
- `CLAUDE_CODE_USE_BEDROCK=1`
- `AWS_REGION=<region>`
- `AWS_PROFILE=<profile>`
- `ANTHROPIC_MODEL=<model>`

### 1C. Add `RequiredEnvVars()` function (`internal/config/provider.go`)

Returns the list of env vars that MUST be set for a given provider+auth config.

```go
type EnvRequirement struct {
    Key      string
    Desc     string
    Required bool   // true = must be set, false = optional but recommended
    Alts     []string // alternative keys (e.g. GITHUB_PAT for GITHUB_TOKEN)
}

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
        case BedrockAuthIAMRole:
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
```

### 1D. Add `ValidateWorkerEnv()` function (`internal/config/provider.go`)

Takes the current worker env (fetched from API) and validates against requirements:

```go
type EnvValidationResult struct {
    Valid    bool
    Missing  []EnvRequirement
    Warnings []string
    Provider Provider
    Auth     BedrockAuthMethod
}

func ValidateWorkerEnv(pc *ProviderConfig, currentEnv map[string]string) *EnvValidationResult {
    // ... check each required var is present in currentEnv ...
    // For Bedrock+IAM role: also check CLAUDE_CODE_USE_BEDROCK == "1" not just set
    // For Bedrock: warn if model ID doesn't look like a Bedrock ID (should contain "anthropic")
}
```

### 1E. Update `provider setup` command (`internal/commands/config_provider.go`)

After provider selection, if Bedrock:
1. Ask auth method: `How does the worker authenticate with AWS?`
   - `1) iam-role    — EC2 instance profile or ECS task role (recommended)`
   - `2) long-term-keys — AWS access key + secret`
   - `3) profile     — Named AWS profile (~/.aws/credentials or SSO)`
2. For `long-term-keys`: prompt for AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY, set them as worker env vars
3. For `profile`/`sso`: prompt for profile name
4. For `iam-role`: no extra prompts

Non-interactive flags: `--auth-method iam-role|long-term-keys|profile` plus `--aws-key`, `--aws-secret`, `--aws-profile`

After setting everything up, **automatically run inference health check**:
1. Call the API server `POST /workers/worker-1/inference-check` 
2. Show result: ✅ inference working / ❌ inference failed (with error details)

### 1F. Update `provider show` (`internal/commands/config_provider.go`)

Show auth method + env validation status. Example output:

```
Provider Configuration
  Provider:    bedrock
  Auth Method: iam-role
  Model:       us.anthropic.claude-opus-4-6-v1
  AWS Region:  us-east-1

Environment Validation
  ✓ CLAUDE_CODE_USE_BEDROCK = 1
  ✓ AWS_REGION = us-east-1
  ✓ ANTHROPIC_MODEL = us.anthropic.claude-opus-4-6-v1
  ✓ AWS credentials: inherited (IAM role)
```

### 1G. Update `doctor` command (`internal/commands/doctor.go`)

Add a new `checkProviderCredentials()` function:
- For Bedrock: verify AWS creds work (via API endpoint)
- For Anthropic: verify API key is set
- For LiteLLM: verify proxy URL is reachable
- Run inference health check as part of doctor

### 1H. Add `--check` flag to `provider set` command

`reposwarm config provider set bedrock --check` — quick-switch + verify inference works.

## Part 2: API Server Changes (TypeScript — /tmp/reposwarm-api)

### 2A. Add `POST /workers/:id/inference-check` endpoint (`src/routes/workers.ts`)

This endpoint:
1. Reads the worker's `.env` file to determine provider + model
2. Makes a minimal inference call based on provider:
   - **Anthropic**: `POST https://api.anthropic.com/v1/messages` with the API key
   - **Bedrock**: `aws bedrock-runtime converse` with the configured model + region
   - **LiteLLM**: `POST <proxy_url>/v1/messages`
3. Uses a tiny prompt: `"Say OK"` with `max_tokens: 10`
4. Returns result:

```json
{
  "data": {
    "success": true,
    "provider": "bedrock",
    "model": "us.anthropic.claude-opus-4-6-v1",
    "authMethod": "iam-role",
    "latencyMs": 842,
    "response": "OK"
  }
}
```

Or on failure:
```json
{
  "data": {
    "success": false,
    "provider": "bedrock",
    "model": "us.anthropic.claude-opus-4-6-v1",
    "authMethod": "iam-role",
    "error": "AccessDeniedException: User is not authorized to perform bedrock:InvokeModel",
    "hint": "Check IAM role has bedrock:InvokeModel permission for the model"
  }
}
```

**Implementation:**
- Use `@aws-sdk/client-bedrock-runtime` for Bedrock (already in node_modules or add it)
- Use native `fetch` for Anthropic API
- Use native `fetch` for LiteLLM proxy
- Read provider from worker .env: if `CLAUDE_CODE_USE_BEDROCK=1` → bedrock, if `ANTHROPIC_BASE_URL` set → litellm, else → anthropic

### 2B. Update `REQUIRED_ENV_VARS` to be provider-aware (`src/routes/workers.ts`)

Currently hardcoded to always require `ANTHROPIC_API_KEY`. Make it dynamic:

```typescript
function getRequiredEnvVars(envVars: Record<string, string>): RequiredEnvVar[] {
  const isBedrock = envVars['CLAUDE_CODE_USE_BEDROCK'] === '1'
  const isLiteLLM = !!envVars['ANTHROPIC_BASE_URL']
  
  const common = [
    { key: 'ANTHROPIC_MODEL', desc: 'Model ID for LLM calls', alts: ['CLAUDE_MODEL', 'MODEL_ID'] },
  ]
  
  if (isBedrock) {
    return [
      ...common,
      { key: 'CLAUDE_CODE_USE_BEDROCK', desc: 'Bedrock mode flag (must be 1)', alts: [] },
      { key: 'AWS_REGION', desc: 'AWS region for Bedrock', alts: ['AWS_DEFAULT_REGION'] },
      // AWS_ACCESS_KEY_ID only required if not using IAM role
      // We detect this: if neither KEY nor PROFILE is set, assume IAM role (which is fine)
    ]
  }
  
  if (isLiteLLM) {
    return [
      ...common,
      { key: 'ANTHROPIC_BASE_URL', desc: 'LiteLLM proxy URL', alts: [] },
    ]
  }
  
  // Default: Anthropic direct
  return [
    ...common,
    { key: 'ANTHROPIC_API_KEY', desc: 'Anthropic API key', alts: [] },
  ]
}
```

### 2C. Add error hints for common failures

Map known error patterns to helpful hints:
- `AccessDeniedException` → "IAM role/user needs `bedrock:InvokeModel` permission"
- `ResourceNotFoundException` → "Model ID not found in this region. Check model availability."
- `ValidationException` → "Invalid model ID format. Bedrock IDs look like `us.anthropic.claude-*`"
- `ExpiredTokenException` → "AWS credentials expired. Refresh SSO or rotate keys."
- `401` / `authentication_error` → "Invalid Anthropic API key"

## Testing

### CLI Tests (`internal/config/provider_test.go`)
Add tests for:
- `RequiredEnvVars()` with each provider+auth combo
- `ValidateWorkerEnv()` with missing/present vars
- `WorkerEnvVars()` with each auth method

### API Tests
Add tests in `tests/` for the inference-check endpoint (mock the LLM calls).

## Build & Verify
```bash
# CLI
cd /tmp/reposwarm-cli && go build -o reposwarm ./cmd/reposwarm && go test ./... -count=1

# API
cd /tmp/reposwarm-api && npm run build && npm test
```
