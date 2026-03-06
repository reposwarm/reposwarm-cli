package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newUpgradeCmd(currentVersion string) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     "upgrade [component]",
		Aliases: []string{"update"},
		Short:   "Upgrade RepoSwarm components (cli, api, ui, worker, all)",
		Long: `Upgrade RepoSwarm components to their latest versions.

Components:
  cli  (default)  Upgrade the CLI binary from GitHub releases
  api             Pull latest API Docker image and restart
  ui              Pull latest UI Docker image and restart
  worker          Pull latest Worker Docker image and restart
  all             Upgrade CLI + all Docker services

For local installations (Docker Compose), upgrading api/ui/worker pulls
the latest images from ghcr.io/reposwarm/* and recreates the containers.

For remote installations, checks version compatibility between the CLI
and the running services.

Examples:
  reposwarm upgrade           # Upgrade CLI only (or all if local)
  reposwarm upgrade cli       # Upgrade CLI binary
  reposwarm upgrade api       # Pull latest API image + restart
  reposwarm upgrade all       # Upgrade CLI + all Docker services
  reposwarm upgrade --force   # Force pull even if up to date`,
		RunE: func(cmd *cobra.Command, args []string) error {
			component := "cli"
			if len(args) > 0 {
				component = args[0]
			}

			// For local installations, default to "all" when no component specified
			if len(args) == 0 {
				cfg, cfgErr := config.Load()
				if cfgErr == nil && isLocalInstall(cfg) {
					component = "all"
					if !flagJSON && !flagAgent {
						output.Infof("Local installation detected — upgrading all components")
					}
				}
			}

			switch component {
			case "cli", "":
				return upgradeCLI(currentVersion, force)
			case "api", "ui", "worker":
				return upgradeDockerService(component, force)
			case "all":
				if err := upgradeCLI(currentVersion, force); err != nil {
					output.F.Warning(fmt.Sprintf("CLI upgrade failed: %v", err))
				}
				if err := upgradeDockerServices(force); err != nil {
					output.F.Warning(fmt.Sprintf("Service upgrade failed: %v", err))
				}
				return nil
			default:
				return fmt.Errorf("unknown component: %s (use: cli, api, ui, worker, all)", component)
			}
		},
		ValidArgs: []string{"cli", "api", "ui", "worker", "all"},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force pull even if up to date")
	return cmd
}

func upgradeCLI(currentVersion string, force bool) error {
	if !flagJSON {
		output.F.Section("RepoSwarm CLI Upgrade")
		fmt.Printf("  Current version: %s\n", output.Cyan("v"+currentVersion))
	}

	latestVer, downloadURL, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}

	if flagJSON {
		return output.JSON(map[string]any{
			"current":     currentVersion,
			"latest":      latestVer,
			"updateAvail": latestVer != currentVersion,
			"downloadUrl": downloadURL,
		})
	}

	fmt.Printf("  Latest version:  %s\n", output.Cyan("v"+latestVer))

	if latestVer == currentVersion && !force {
		fmt.Printf("\n  %s\n\n", output.Green("Already up to date!"))
		return nil
	}

	if latestVer == currentVersion && force {
		output.Infof("Reinstalling v%s (--force)", currentVersion)
	} else {
		output.Infof("Upgrading v%s → v%s", currentVersion, latestVer)
	}

	fmt.Printf("  Downloading...")
	tmpFile, err := downloadBinary(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmpFile)
	fmt.Printf(" done\n")

	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding current binary: %w", err)
	}
	binPath, err = filepath.EvalSymlinks(binPath)
	if err != nil {
		return fmt.Errorf("resolving binary path: %w", err)
	}

	fmt.Printf("  Installing to %s...", binPath)
	if err := safeReplaceBinary(tmpFile, binPath); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}
	fmt.Printf(" done\n\n")

	output.F.Success(fmt.Sprintf("reposwarm v%s installed — restart your shell or run 'reposwarm version' to verify", latestVer))

	changes, err := getChangelog(currentVersion, latestVer)
	if err == nil && len(changes) > 0 {
		fmt.Println()
		output.F.Section("What's New")
		for _, line := range changes {
			fmt.Printf("  %s\n", line)
		}
		fmt.Println()
	}

	return nil
}

// upgradeDockerService pulls latest Docker image for a single service and recreates it.
func upgradeDockerService(svc string, force bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if !isLocalInstall(cfg) {
		return checkRemoteCompatibility(svc)
	}

	return pullAndRestart(cfg, svc, force)
}

// upgradeDockerServices pulls latest images for all services and recreates them.
func upgradeDockerServices(force bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if !isLocalInstall(cfg) {
		return checkRemoteCompatibility("all")
	}

	composeDir := getComposeDir(cfg)
	if composeDir == "" {
		return fmt.Errorf("cannot find docker-compose.yml — was this installed with 'reposwarm new --local'?")
	}

	if !flagJSON {
		output.F.Section("Upgrading Docker Services")
	}

	// Pull all images
	if !flagJSON {
		output.Infof("Pulling latest images...")
	}
	pullArgs := []string{"compose", "pull"}
	if force {
		pullArgs = append(pullArgs, "--ignore-pull-failures")
	}
	pullCmd := exec.Command("docker", pullArgs...)
	pullCmd.Dir = composeDir
	pullOut, err := pullCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose pull failed: %w\n%s", err, string(pullOut))
	}

	// Recreate containers with new images
	if !flagJSON {
		output.Infof("Recreating containers...")
	}
	upCmd := exec.Command("docker", "compose", "up", "-d", "--remove-orphans")
	upCmd.Dir = composeDir
	upOut, err := upCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose up failed: %w\n%s", err, string(upOut))
	}

	if flagJSON {
		return output.JSON(map[string]any{
			"action":   "upgrade",
			"services": []string{"api", "worker", "ui"},
			"status":   "updated",
		})
	}

	output.F.Success("All services updated to latest images")
	output.Infof("Containers recreated with latest images from ghcr.io/reposwarm/*")
	return nil
}

// pullAndRestart pulls latest image for a specific service and recreates it.
func pullAndRestart(cfg *config.Config, svc string, force bool) error {
	composeDir := getComposeDir(cfg)
	if composeDir == "" {
		return fmt.Errorf("cannot find docker-compose.yml — was this installed with 'reposwarm new --local'?")
	}

	if !flagJSON {
		output.F.Section(fmt.Sprintf("Upgrading %s", strings.ToUpper(svc)))
		output.Infof("Pulling latest image for %s...", svc)
	}

	// Pull the specific service image
	pullCmd := exec.Command("docker", "compose", "pull", svc)
	pullCmd.Dir = composeDir
	pullOut, err := pullCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose pull %s failed: %w\n%s", svc, err, string(pullOut))
	}

	// Recreate just this service
	if !flagJSON {
		output.Infof("Recreating %s container...", svc)
	}
	upCmd := exec.Command("docker", "compose", "up", "-d", "--no-deps", svc)
	upCmd.Dir = composeDir
	upOut, err := upCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose up %s failed: %w\n%s", svc, err, string(upOut))
	}

	if flagJSON {
		return output.JSON(map[string]any{
			"action":  "upgrade",
			"service": svc,
			"status":  "updated",
		})
	}

	output.F.Success(fmt.Sprintf("%s updated to latest image", strings.ToUpper(svc)))
	return nil
}

// checkRemoteCompatibility checks version compatibility between CLI and remote services.
func checkRemoteCompatibility(svc string) error {
	if !flagJSON {
		output.F.Section("Version Compatibility Check")
	}

	client, err := getClient()
	if err != nil {
		return fmt.Errorf("connecting to API: %w", err)
	}

	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := client.Get(ctx(), "/health", &health); err != nil {
		return fmt.Errorf("cannot reach API: %w", err)
	}

	if flagJSON {
		return output.JSON(map[string]any{
			"mode":       "remote",
			"apiVersion": health.Version,
			"note":       "Remote services are managed by their deployment pipeline. Use docker compose pull to upgrade Docker-based deployments.",
		})
	}

	if flagAgent {
		fmt.Printf("Remote API version: %s. Services are managed by deployment pipeline, not CLI upgrade.\n", health.Version)
		return nil
	}

	output.Infof("API version: %s", health.Version)
	fmt.Println()
	output.Infof("Remote services are managed by their deployment pipeline.")
	output.Infof("To upgrade a Docker-based remote deployment:")
	fmt.Println()
	fmt.Printf("  cd <deploy-dir> && docker compose pull && docker compose up -d\n\n")
	output.Infof("The CLI only upgrades local Docker Compose installations.")
	output.Infof("Use 'reposwarm upgrade cli' to upgrade the CLI binary itself.")

	return nil
}

// getComposeDir finds the docker-compose.yml directory for a local installation.
func getComposeDir(cfg *config.Config) string {
	if cfg.InstallDir != "" {
		dir := filepath.Join(cfg.InstallDir, "temporal")
		if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err == nil {
			return dir
		}
	}
	// Try default locations
	home, _ := os.UserHomeDir()
	for _, candidate := range []string{
		filepath.Join(home, "reposwarm", "temporal"),
		filepath.Join(home, "repo", "repos", "reposwarm", "temporal"),
	} {
		if _, err := os.Stat(filepath.Join(candidate, "docker-compose.yml")); err == nil {
			return candidate
		}
	}
	return ""
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func getLatestRelease() (version, downloadURL string, err error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(config.CLIReleasesAPI + "/latest")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", err
	}

	version = release.TagName
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}

	binaryName := fmt.Sprintf("reposwarm-%s-%s", runtime.GOOS, runtime.GOARCH)
	for _, asset := range release.Assets {
		if asset.Name == binaryName {
			return version, asset.BrowserDownloadURL, nil
		}
	}

	return version, "", fmt.Errorf("no binary found for %s in release assets", binaryName)
}

// getChangelog fetches release notes between the old and new version from GitHub.
// Returns one-liner changes (commit messages) or release body lines.
func getChangelog(oldVersion, newVersion string) ([]string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Fetch the new release body — it contains the changelog
	resp, err := client.Get(fmt.Sprintf("%s/tags/v%s", config.CLIReleasesAPI, newVersion))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var release struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	if release.Body == "" {
		return nil, nil
	}

	// Parse the body — extract lines starting with "•" or "-" (changelog items)
	var changes []string
	for _, line := range splitLines(release.Body) {
		trimmed := trimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		// Skip headers and install instructions
		if len(trimmed) > 0 && (trimmed[0] == '#' || contains(trimmed, "Install:") || contains(trimmed, "Upgrade:") || trimmed == "---") {
			continue
		}
		// Include bullet points
		if len(trimmed) > 0 && (trimmed[0] == '-' || trimmed[0] == '*' || (len(trimmed) >= 3 && trimmed[:3] == "• ")) {
			changes = append(changes, trimmed)
		}
	}

	// Limit to 20 lines
	if len(changes) > 20 {
		changes = changes[:20]
		changes = append(changes, fmt.Sprintf("  ... and more (see %s/tag/v%s)", config.CLIReleasesURL, newVersion))
	}

	return changes, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func downloadBinary(url string) (string, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	tmp, err := os.CreateTemp("", "reposwarm-upgrade-*")
	if err != nil {
		return "", err
	}

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	tmp.Close()
	os.Chmod(tmp.Name(), 0755)

	return tmp.Name(), nil
}

// isLocalInstall checks if the current config points to a local installation
// (InstallDir is set, or API URL points to localhost).
func isLocalInstall(cfg *config.Config) bool {
	if cfg.InstallDir != "" {
		return true
	}
	url := cfg.APIUrl
	return strings.Contains(url, "localhost") || strings.Contains(url, "127.0.0.1")
}

// safeReplaceBinary replaces the binary without corrupting the running process.
// On macOS/Linux, a running binary can be renamed but not overwritten safely.
// Strategy: rename old → write new → delete old.
func safeReplaceBinary(src, dst string) error {
	newData, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	dir := filepath.Dir(dst)
	base := filepath.Base(dst)
	oldPath := filepath.Join(dir, "."+base+".old")

	// Remove any leftover from previous upgrade
	os.Remove(oldPath)

	// Rename running binary out of the way (safe on macOS/Linux)
	if err := os.Rename(dst, oldPath); err != nil {
		// Can't rename — try direct write as last resort
		if err := os.WriteFile(dst, newData, 0755); err != nil {
			return fmt.Errorf("cannot replace %s (try: sudo reposwarm upgrade): %w", dst, err)
		}
		return nil
	}

	// Write new binary to the original path
	if err := os.WriteFile(dst, newData, 0755); err != nil {
		// Rollback
		os.Rename(oldPath, dst)
		return fmt.Errorf("failed to write new binary: %w", err)
	}

	// Clean up old binary (best effort — may fail if still running, that's fine)
	go func() {
		time.Sleep(2 * time.Second)
		os.Remove(oldPath)
	}()

	return nil
}
