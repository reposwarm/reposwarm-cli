# RepoSwarm E2E Test Report

## Header
- **Test Date**: 2026-03-08
- **Agent Model**: Claude Sonnet 4.5 (us.anthropic.claude-sonnet-4-5-20250929-v1:0)
- **Environment**: AWS EC2 (linux/arm64, IAM role auth)
- **Component Versions**:
  - **CLI**: 1.3.156 ✓ (expected: 1.3.156)
  - **API**: 1.0.0 (from status command)
  - **Worker**: Image tag `latest`
  - **UI**: Image tag `latest`
  - **Ask CLI**: 0.2.0 ✓ (expected: 0.2.0)

## Executive Summary
- **Overall Score**: 5/10
- **Pass/Fail Verdict**: ❌ **FAIL** - Critical blocker: arch-hub environment variable not being read

The installation and basic operations work well, but a **critical bug prevents investigations from completing successfully**. The worker does not read the `ARCH_HUB_BASE_URL` environment variable, causing all investigations to fail at the final arch-hub push step. Without this fix, the system cannot complete its core workflow end-to-end.

**Positive highlights**: Provider configuration is smooth, CLI UX is good, Ask CLI works perfectly, and Claude API integration via Bedrock is flawless.

---

## Scenario Results

### Scenario 1: Install & Setup ✅ PASS
**Commands Run:**
```bash
curl -fsSL https://raw.githubusercontent.com/reposwarm/reposwarm-cli/main/install.sh | sh
reposwarm version --for-agent
reposwarm --help
reposwarm new --local --for-agent
reposwarm status --for-agent
docker ps
reposwarm services --for-agent
```

**Output Highlights:**
- CLI installed successfully to `/home/ubuntu/.local/bin/reposwarm`
- Version 1.3.156 confirmed (matches expected)
- Local instance creation took ~1 minute
- All containers started successfully (6/7 healthy, worker exited as expected before config)
- API responded at http://localhost:3000/v1
- UI responded at http://localhost:3001

**Result**: ✅ **PASS** - Installation smooth, all components started correctly

**Notes:**
- Worker container initially exited (expected - needs provider config)
- Docker compose orchestration worked flawlessly
- Clear, helpful output messages throughout

---

### Scenario 2: Provider & Arch-Hub Configuration ✅ PASS (with workaround needed)
**Commands Run:**
```bash
reposwarm config provider setup --provider bedrock --auth-method iam-role --region us-east-1 --model sonnet --non-interactive --for-agent
reposwarm config worker-env set ARCH_HUB_BASE_URL https://github.com/reposwarm/e2e-arch-hub --for-agent
reposwarm config worker-env set GITHUB_TOKEN 'github_pat_11AA...' --for-agent
reposwarm restart worker --for-agent
reposwarm doctor --for-agent
```

**Output Highlights:**
- Provider configuration succeeded instantly
- Model resolved to: `us.anthropic.claude-sonnet-4-5-20250514-v1:0`
- Worker environment variables written to `/home/ubuntu/.reposwarm/temporal/worker.env`
- Worker restarted and became healthy
- Doctor check: 23 passed, 7 warnings

**Result**: ✅ **PASS** - Configuration commands work correctly

**Issues Discovered:**
- ⚠️ **BUG FOUND**: Worker does not actually read `ARCH_HUB_BASE_URL` (see bug list)
- Variables appear to be set correctly in the env file but aren't used by the worker

---

### Scenario 3: Investigation (single repo) ⚠️ PARTIAL PASS
**Commands Run:**
```bash
reposwarm repos add https://github.com/jonschlinkert/is-odd --for-agent
reposwarm investigate https://github.com/jonschlinkert/is-odd --for-agent
reposwarm workflows progress --for-agent  # polled every 30s
reposwarm workflows list --for-agent
reposwarm workers list --for-agent
docker logs reposwarm-worker --tail 50
```

**Output Highlights:**
- Repository added successfully
- Pre-flight checks passed (6 checks)
- Investigation started successfully
- Worker actively processed 17 investigation steps
- Claude API calls via Bedrock worked perfectly (5-10s response times)
- All analysis steps completed successfully (hl_overview, module_deep_dive, dependencies, etc.)

**Result**: ⚠️ **PARTIAL PASS** - Analysis works, but final step fails

**Critical Issues:**
1. **Progress Counter Broken**: Shows "0/11 (0%)" instead of detailed "N/17 steps" progress
   - Worker logs show steps completing correctly
   - Progress display does not reflect actual work being done

2. **Arch-Hub Push Fails**: After completing all 17 analysis steps, the workflow fails with:
   ```
   ERROR: Failed to save to architecture-hub: Activity task failed
   Git clone failed: Cmd('git') failed due to: exit code(128)
   cmdline: git clone -v -- https://github.com/your-org/architecture-hub.git
   fatal: could not read Username for 'https://github.com': No such device or address
   ```
   - Worker uses hardcoded URL: `https://github.com/your-org/architecture-hub.git`
   - Configured `ARCH_HUB_BASE_URL` is completely ignored

3. **Workflows Stuck in "Running" State**: Cannot complete due to arch-hub push retries
   - No timeout or max retry limit apparent
   - Workflows show as "Running" indefinitely

**Workarounds**: None found - this is a blocking bug

---

### Scenario 4: Ask CLI ✅ PASS
**Commands Run:**
```bash
curl -fsSL https://raw.githubusercontent.com/reposwarm/ask-cli/main/install.sh | sh
ask version
ask --version  # (failed - documented)
ask setup --non-interactive --provider bedrock --auth-method iam-role --region us-east-1 --model sonnet --arch-hub https://github.com/reposwarm/e2e-arch-hub
ask up
ask "What does is-odd do and how does it work?" --for-agent
ask list --for-agent
```

**Output Highlights:**
- Ask CLI v0.2.0 installed to `/usr/local/bin/ask`
- `ask version` works ✅
- `ask --version` fails ❌ (returns "unknown flag" error)
- Setup completed successfully, askbox started via Docker
- Question answered in 30 seconds with 6 tool calls
- Comprehensive, accurate answer about is-odd (2903 characters)
- Retrieved data from pre-existing arch-hub on GitHub

**Result**: ✅ **PASS** - Ask CLI works excellently

**Minor Issues:**
- `ask --version` not supported (only `ask version` works)
- This is inconsistent with standard CLI conventions

---

### Scenario 5: investigate --all (de-duplication) ❌ FAIL
**Commands Run:**
```bash
reposwarm repos add https://github.com/jonschlinkert/is-even --for-agent
reposwarm repos list --for-agent
reposwarm investigate --all --for-agent
reposwarm workflows progress --for-agent
```

**Output Highlights:**
- is-even repo added successfully
- Both repos listed correctly (is-odd, is-even)
- `investigate --all` started investigations for BOTH repos
- Did NOT skip is-odd (which already had running investigations)

**Result**: ❌ **FAIL** - No deduplication observed

**Expected Behavior**: Should skip is-odd since it was already investigated
**Actual Behavior**: Started new investigation for is-odd anyway

**Possible Explanations**:
1. Deduplication only works for *completed* workflows (not running ones)
2. Deduplication not implemented yet
3. Deduplication broken by arch-hub push failures

---

### Scenario 6: Workflow Status & Error Handling ⚠️ PARTIAL PASS
**Commands Run:**
```bash
reposwarm workflows list --for-agent
reposwarm workflows status investigate-single-is-odd-1772961819 --for-agent
reposwarm workflows status investigate-single-https://github.com/jonschlinkert/is-odd-1772961400 --for-agent
reposwarm errors --for-agent
```

**Output Highlights:**
- `workflows list` works correctly ✅
- `workflows status` works for simple IDs (e.g., `investigate-single-is-odd-1772961819`) ✅
- `workflows status` returns 404 for URL-containing IDs ❌
- `errors` command shows stall warnings but says "No errors found" (confusing)

**Result**: ⚠️ **PARTIAL PASS** - Works for some workflow IDs, not all

**Issues:**
1. **Workflow Status 404**: IDs with URLs (from `workflows list`) don't work with `workflows status`
   - Error: `ERROR: not found (404): /workflows/investigate-single-https://github.com/jonschlinkert/is-odd-1772961400`
   - URL encoding/escaping issue?

2. **Errors Command Misleading**: Shows stall warnings but concludes with "No errors found 🎉"
   - Should either show errors or not show warnings

---

### Scenario 7: Diagnostics & Cleanup ⚠️ PARTIAL PASS
**Commands Run:**
```bash
reposwarm doctor --for-agent
reposwarm logs worker --for-agent --tail 30
reposwarm stop --for-agent  # (failed - documented)
reposwarm teardown --force --for-agent
docker ps --filter "name=reposwarm"
```

**Output Highlights:**
- Doctor check: 23 passed, 6 warnings
- All core systems healthy (API, Temporal, DynamoDB, Worker)
- Worker logs accessible and detailed
- `reposwarm stop --for-agent` requires a service name (does NOT stop all services)
- `reposwarm teardown --force --for-agent` successfully stopped and removed all containers

**Result**: ⚠️ **PARTIAL PASS** - Diagnostics work, but stop command doesn't match spec

**Issues:**
1. **Stop Command Doesn't Stop All**: `reposwarm stop --for-agent` returns error requiring service name
   - Expected: Stop ALL services without requiring a service name
   - Actual: Returns usage help showing service names required
   - Help text shows `[service]` as optional but it's actually required

---

## Bug List

### CRITICAL Severity

#### 1. ARCH_HUB_BASE_URL Environment Variable Not Read
**Severity**: CRITICAL
**Description**: Worker does not read the `ARCH_HUB_BASE_URL` environment variable and uses a hardcoded default URL instead.

**Repro Steps**:
1. Configure provider and worker env: `reposwarm config worker-env set ARCH_HUB_BASE_URL https://github.com/reposwarm/e2e-arch-hub --for-agent`
2. Verify env file shows correct URL: `cat /home/ubuntu/.reposwarm/temporal/worker.env`
3. Start investigation: `reposwarm investigate <repo> --for-agent`
4. Check worker logs: `docker logs reposwarm-worker 2>&1 | grep arch`

**Expected**: Worker clones from `https://github.com/reposwarm/e2e-arch-hub`
**Actual**: Worker attempts to clone from `https://github.com/your-org/architecture-hub.git`

**Impact**: **Blocks all investigations from completing**. Analysis completes successfully but workflow fails at final arch-hub push step.

**Workaround**: None

**Log Evidence**:
```
2026-03-08 09:17:55,862 - temporalio.activity - INFO - Cloning repository: https://github.com/your-org/architecture-hub.git
2026-03-08 09:17:55,964 - temporalio.activity - ERROR - Git clone failed: Cmd('git') failed due to: exit code(128)
  stderr: 'fatal: could not read Username for 'https://github.com': No such device or address'
```

---

### HIGH Severity

#### 2. Workflows Stuck in Running State After Arch-Hub Failure
**Severity**: HIGH
**Description**: When arch-hub push fails, workflows remain in "Running" state indefinitely with no timeout or max retry limit.

**Repro Steps**:
1. Start investigation with misconfigured arch-hub
2. Wait for investigation to complete analysis steps
3. Check workflow status: `reposwarm workflows list --for-agent`

**Expected**: Workflow should fail after N retries or timeout
**Actual**: Workflow stays "Running" indefinitely, retrying arch-hub push forever

**Impact**: Accumulates stale workflows, confuses users about actual system state

**Workaround**: Manual cleanup via docker restart or teardown

---

#### 3. Investigation Progress Counter Shows 0% Despite Active Work
**Severity**: HIGH
**Description**: `reposwarm workflows progress --for-agent` shows "0/11 (0%)" even when worker is actively completing investigation steps.

**Repro Steps**:
1. Start investigation
2. Poll progress: `reposwarm workflows progress --for-agent`
3. Compare with worker logs: `docker logs reposwarm-worker --tail 50`

**Expected**: Progress shows "N/17 steps (X%)" with increasing N
**Actual**: Always shows "0/11 (0%)" regardless of actual progress

**Impact**: Users cannot monitor investigation progress; appears stuck even when working

**Workaround**: Check worker logs directly

---

#### 4. Workflow Status Returns 404 for URL-Containing Workflow IDs
**Severity**: HIGH
**Description**: `reposwarm workflows status <workflow-id>` returns 404 for workflow IDs that contain URLs.

**Repro Steps**:
1. Get workflow ID from list: `reposwarm workflows list --for-agent`
2. Copy a workflow ID like `investigate-single-https://github.com/jonschlinkert/is-odd-1772961400`
3. Run: `reposwarm workflows status investigate-single-https://github.com/jonschlinkert/is-odd-1772961400 --for-agent`

**Expected**: Returns workflow details
**Actual**: `ERROR: not found (404): /workflows/investigate-single-https://github.com/jonschlinkert/is-odd-1772961400`

**Impact**: Cannot check status for workflows with URL-based IDs

**Workaround**: Only works for simple IDs (e.g., `investigate-single-is-odd-1772961819`)

---

### MEDIUM Severity

#### 5. reposwarm stop Requires Service Name Despite Help Showing Optional
**Severity**: MEDIUM
**Description**: The `reposwarm stop` command requires a service name argument, but the help text shows `[service]` as optional.

**Repro Steps**:
1. Run: `reposwarm stop --for-agent`
2. Observe error message

**Expected**: Stops all services when no service name provided
**Actual**: Returns error: "💡 reposwarm stop <service>"

**Impact**: UX confusion, command doesn't work as documented

**Workaround**: Use `reposwarm teardown --force --for-agent` to stop all

---

#### 6. investigate --all Does Not Skip Already-Investigated Repos
**Severity**: MEDIUM
**Description**: Running `reposwarm investigate --all` starts new investigations for repos that already have running investigations.

**Repro Steps**:
1. Investigate repo: `reposwarm investigate https://github.com/jonschlinkert/is-odd --for-agent`
2. Add another repo: `reposwarm repos add https://github.com/jonschlinkert/is-even --for-agent`
3. Run: `reposwarm investigate --all --for-agent`
4. Check progress: `reposwarm workflows progress --for-agent`

**Expected**: Skips is-odd (already investigated), only starts is-even
**Actual**: Starts investigations for BOTH repos

**Impact**: Duplicate investigations, wasted resources

**Workaround**: Manually track which repos have been investigated

---

### LOW Severity

#### 7. ask --version Not Supported
**Severity**: LOW
**Description**: Ask CLI supports `ask version` but not `ask --version` (common CLI convention).

**Repro Steps**:
1. Run: `ask version` ✅ works
2. Run: `ask --version` ❌ fails with "unknown flag: --version"

**Expected**: Both commands return version
**Actual**: Only `ask version` works

**Impact**: Minor UX inconsistency

**Workaround**: Use `ask version` instead

---

#### 8. reposwarm errors Shows Warnings But Says "No errors found"
**Severity**: LOW
**Description**: The `reposwarm errors` command displays stall warnings but concludes with "OK: No errors found 🎉".

**Repro Steps**:
1. Run: `reposwarm errors --for-agent`
2. Observe output showing warnings followed by "No errors found"

**Expected**: Either show errors/warnings and don't say "OK", or truly show nothing if OK
**Actual**: Conflicting message

**Impact**: Confusing UX

**Workaround**: Ignore the "OK" message and focus on warnings

---

## What Worked Well

### Installation & Setup
- **Smooth Installation**: Both reposwarm and ask CLI installed flawlessly with simple curl commands
- **Fast Bootstrap**: Local instance setup completed in ~1 minute
- **Clear Output**: Installation messages were helpful and actionable
- **Version Verification**: `reposwarm version --for-agent` provided clean, parseable output

### Provider Configuration
- **Streamlined Setup**: Provider configuration with Bedrock was quick and painless
- **IAM Role Support**: Automatic IAM role detection worked perfectly (no API keys needed)
- **Model Resolution**: "sonnet" alias correctly resolved to `us.anthropic.claude-sonnet-4-5-20250514-v1:0`
- **Automatic Restart**: Worker restarted automatically after provider config

### Core Analysis Engine
- **Claude API Integration**: Bedrock API calls worked flawlessly with 5-10s response times
- **Step Processing**: Worker correctly executed all 17 investigation steps in sequence
- **Prompt Caching**: DynamoDB-based prompt caching working as designed
- **Detailed Logging**: Worker logs are comprehensive and useful for debugging

### Ask CLI
- **Excellent UX**: Setup and usage were intuitive and fast
- **Fast Responses**: Questions answered in ~30 seconds
- **High Quality**: Generated comprehensive, accurate architecture answers
- **Tool Integration**: 6 tool calls per question showed good reasoning

### Diagnostic Tools
- **Doctor Command**: Thorough health checks with clear pass/fail indicators
- **Helpful Warnings**: Identified stalled workflows and upgrade opportunities
- **Log Access**: Easy access to worker logs via CLI

---

## Performance Notes

### Installation Times
- **reposwarm CLI**: 2-3 seconds
- **ask CLI**: 1-2 seconds
- **Local instance bootstrap**: ~60 seconds (first run with image pulls)

### Investigation Duration
- **Analysis Phase**: ~3-5 minutes for 17 steps (small repos like is-odd)
- **Per-Step Average**: ~10-20 seconds per Claude API call
- **Arch-Hub Push**: Would be <5 seconds if working

### API Response Times
- **reposwarm status**: 8-15ms latency
- **Bedrock Claude API**: 5-10 seconds per call (streaming not observed)
- **Ask CLI questions**: ~30 seconds end-to-end

### Resource Usage
- **Memory**: Docker containers consumed ~2GB total
- **CPU**: Moderate usage during API calls, idle otherwise
- **Disk**: Minimal (<500MB for all containers)

---

## Security Observations

### Positive
- **IAM Role Auth**: Properly uses IAM roles instead of hardcoded credentials
- **Token Masking**: Sensitive tokens properly masked in output (e.g., `***...08e346`)
- **Environment Isolation**: Worker runs in isolated Docker container
- **GitHub Token Handling**: Token stored in env file with restricted permissions

### Concerns
- **Hardcoded URL**: Default arch-hub URL (`https://github.com/your-org/architecture-hub.git`) could leak in logs
- **Error Messages**: Git errors expose full command lines (could leak tokens if passed as args)
- **No HTTPS Verification**: Unclear if git clone operations verify HTTPS certificates

### Recommendations
1. Ensure git operations use credential helpers instead of URL-embedded tokens
2. Add explicit HTTPS verification for all git operations
3. Redact full git command lines from error messages

---

## Final Verdict

### Could an agent complete this without manual fixes?

**❌ NO** - Blocked by critical arch-hub bug

### Specific Blockers

1. **ARCH_HUB_BASE_URL not being read** (CRITICAL)
   - Prevents any investigation from completing successfully
   - No workaround available
   - Must be fixed in worker code

2. **Workflow status 404 for URL IDs** (HIGH)
   - Prevents monitoring of most workflows
   - Workaround exists for simple IDs only

3. **Progress counter broken** (HIGH)
   - Impossible to monitor investigation progress reliably
   - Must rely on logs instead

### What Works Without Fixes
- Installation and setup
- Provider configuration
- Repository management
- Investigation analysis phase (before arch-hub push)
- Ask CLI (using pre-existing arch-hub data)
- Basic diagnostic commands

### What's Completely Broken
- Completing investigations end-to-end
- Pushing results to arch-hub
- Workflow progress monitoring
- Workflow deduplication
- Stopping all services with one command

---

## Recommendations for Next Release

### Must Fix (P0)
1. **Fix ARCH_HUB_BASE_URL reading**: Update worker to actually use the environment variable
2. **Add workflow timeout/max retries**: Prevent infinite retry loops
3. **Fix progress counter**: Show actual step progress (N/17)

### Should Fix (P1)
4. **URL-encode workflow IDs**: Fix 404 for status command with URL IDs
5. **Implement workflow deduplication**: Skip already-investigated repos
6. **Fix stop command**: Accept no args to stop all services

### Nice to Have (P2)
7. **Add `ask --version` support**: Standard CLI convention
8. **Improve errors command output**: Consistent messaging
9. **Add streaming support**: For faster Claude API responses

---

## Test Artifacts

- **Worker Logs**: Available at `/home/ubuntu/.reposwarm/logs/worker.log`
- **Worker Env**: `/home/ubuntu/.reposwarm/temporal/worker.env`
- **Docker Compose**: `/home/ubuntu/.reposwarm/temporal/docker-compose.yml`
- **Install Log**: `/home/ubuntu/.reposwarm/logs/install-20260308-091324.log`

---

## Conclusion

RepoSwarm shows great promise with excellent UX, smooth installation, and a solid architecture analysis engine. However, the **critical arch-hub environment variable bug completely blocks the core workflow from completing end-to-end**. Once this is fixed, the system should work well for agents and humans alike.

The Ask CLI is production-ready and works excellently. The investigation engine is functionally complete but needs the arch-hub integration fixed to be usable.

**Estimated Fix Time**:
- Arch-hub bug: 2-4 hours (environment variable plumbing)
- Progress counter: 2-3 hours (API aggregation)
- Workflow timeout: 1-2 hours (add Temporal retry policy)
- URL encoding: 1 hour (API route handling)

**Total to unblock**: ~6-10 hours of development work
