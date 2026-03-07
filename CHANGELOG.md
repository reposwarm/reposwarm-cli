# Changelog

## Unreleased

### Bug Fixes

- **doctor: fix Anthropic API key reported as NOT SET for Docker installs** — `checkProviderCredentials()` was fetching environment variables exclusively from the API endpoint (`/workers/worker-1/env?reveal=true`), which returns empty results for Docker-based installations. The fix applies the same Docker-aware pattern already used by `checkWorkerEnv()`: detecting the install type and reading from the local `worker.env` file via `bootstrap.ReadWorkerEnvFile()` for Docker installs, falling back to the API endpoint for source installs.
