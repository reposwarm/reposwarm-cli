package commands

import (
	"bufio"
	"context"
	"net/http"
	"sort"
	"time"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/reposwarm/reposwarm-cli/internal/bootstrap"
	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newNewCmd() *cobra.Command {
	var dir string
	var agentMode bool
	var guideOnly bool
	var forceMode bool
	var localMode bool

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Set up a new local RepoSwarm installation",
		Long: `Detects your local environment, generates a tailored installation guide,
and optionally hands it to a coding agent (Claude Code, Codex, etc.) for 
interactive setup.

Use --local to automatically set up and start all services locally
(Temporal, API, Worker, UI) via Docker Compose using pre-built images.

Examples:
  reposwarm new                    # Interactive setup in ~/.reposwarm
  reposwarm new --local            # Automated local setup (start everything)
  reposwarm new --dir ~/projects   # Custom install directory
  reposwarm new --agent            # Auto-launch coding agent
  reposwarm new --guide-only       # Just generate the guide file`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Detect environment
			env := bootstrap.Detect()

			if dir == "" {
				dir = env.InstallDir()
			}

			missing := env.MissingDeps()

			// --local mode: automated setup
			if localMode {
				// Check if there's already a local install
				if existing := detectExistingInstall(dir, flagJSON, flagAgent, forceMode); existing {
					return nil
				}
				cliCfg, _ := config.Load()
				bsCfg := &bootstrap.Config{
					WorkerRepoURL:  cliCfg.EffectiveWorkerRepoURL(),
					APIRepoURL:     cliCfg.EffectiveAPIRepoURL(),
					UIRepoURL:      cliCfg.EffectiveUIRepoURL(),
					DynamoDBTable:  cliCfg.EffectiveDynamoDBTable(),
					DefaultModel:   cliCfg.EffectiveModel(),
					TemporalPort:   cliCfg.EffectiveTemporalPort(),
					TemporalUIPort: cliCfg.EffectiveTemporalUIPort(),
					APIPort:        cliCfg.EffectiveAPIPort(),
					UIPort:         cliCfg.EffectiveUIPort(),
					Region:         cliCfg.Region,
				}
				if bsCfg.Region == "" {
					bsCfg.Region = env.AWSRegion
				}

				// JSON / agent modes — skip plan, go straight to setup
				if flagJSON {
					printer := &jsonPrinter{}
					result, err := bootstrap.SetupLocal(env, dir, bsCfg, printer)
					if err != nil {
						return output.JSON(result)
					}
					// Persist installDir + token so 'start/stop/restart' find it
					cliCfg.InstallDir = dir
					cliCfg.InstallType = "docker"
					cliCfg.APIUrl = fmt.Sprintf("http://localhost:%s/v1", bsCfg.APIPort)
					cliCfg.APIToken = result.Token
					_ = config.Save(cliCfg)
					return output.JSON(result)
				}
				if flagAgent {
					printer := &fmtPrinter{}
					result, err := bootstrap.SetupLocal(env, dir, bsCfg, printer)
					if err == nil {
						cliCfg.InstallDir = dir
					cliCfg.InstallType = "docker"
						cliCfg.APIUrl = fmt.Sprintf("http://localhost:%s/v1", bsCfg.APIPort)
						cliCfg.APIToken = result.Token
						_ = config.Save(cliCfg)

						fmt.Println()
						fmt.Println("OK: RepoSwarm local environment is running!")
						fmt.Println()
						fmt.Printf("  Temporal UI:  http://localhost:%s\n", bsCfg.TemporalUIPort)
						fmt.Printf("  API Server:   http://localhost:%s\n", bsCfg.APIPort)
						fmt.Printf("  UI:           http://localhost:%s\n", bsCfg.UIPort)
						fmt.Println()
						fmt.Printf("  API Token:    %s\n", result.Token)
					}
					return err
				}

				// ── Human interactive mode ──
				// Show the plan and ask for confirmation
				plan := bootstrap.PlanFromConfig(bsCfg, dir)
				if !showPlanAndConfirm(plan, env, forceMode) {
					fmt.Println()
					output.F.Info("No worries! Run again whenever you're ready. 👋")
					fmt.Println()
					return nil
				}

				// Run setup with spinners
				printer := &spinnerPrinter{}
				result, err := bootstrap.SetupLocal(env, dir, bsCfg, printer)
				if err != nil {
					return err
				}

				// Persist installDir + token so 'start/stop/restart' find the right directory
				cliCfg.InstallDir = dir
					cliCfg.InstallType = "docker"
				cliCfg.APIUrl = fmt.Sprintf("http://localhost:%s/v1", bsCfg.APIPort)
				cliCfg.APIToken = result.Token
				_ = config.Save(cliCfg)

				// ── Post-setup: Provider configuration ──
				fmt.Println()
				output.F.Section("Provider Configuration")
				fmt.Println()
				output.F.Info("RepoSwarm needs an LLM provider to investigate repos.")
				fmt.Println()

				reader := bufio.NewReader(os.Stdin)
				fmt.Print("  Configure provider now? [Y/n] ")
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(answer)

				if answer == "" || strings.ToLower(answer) == "y" || strings.ToLower(answer) == "yes" {
					// Run provider setup inline (calls the same flow as 'config provider setup')
					// NOTE: We call RunE directly instead of Execute() because cobra's Execute()
					// re-parses os.Args from the root, picking up "new" as a subcommand of "provider".
					providerCmd := newConfigProviderCmd()
					// Find 'setup' subcommand
					for _, sub := range providerCmd.Commands() {
						if sub.Name() == "setup" {
							if setupErr := sub.RunE(sub, []string{}); setupErr != nil {
								output.F.Warning(fmt.Sprintf("Provider setup had issues: %v", setupErr))
								output.F.Info("You can configure later with: reposwarm config provider setup")
							} else {
								// Ask about worker restart
								fmt.Println()
								fmt.Print("  Restart worker to apply new settings? [Y/n] ")
								restartAnswer, _ := reader.ReadString('\n')
								restartAnswer = strings.TrimSpace(restartAnswer)
								if restartAnswer == "" || strings.ToLower(restartAnswer) == "y" {
									fmt.Print("  Restarting worker... ")
									restartCmd := newRestartCmd()
									restartCmd.SetArgs([]string{"worker"})
									if restartErr := restartCmd.Execute(); restartErr != nil {
										output.F.Warning(fmt.Sprintf("Restart failed: %v", restartErr))
									} else {
										output.Successf("Worker restarted")
									}
								}
							}
							break
						}
					}
				} else {
					output.F.Info("Skipped. Configure later: reposwarm config provider setup")
				}

				// ── Post-setup: Git provider + Arch-hub ──
				fmt.Println()
				output.F.Section("Git & Architecture Hub")
				fmt.Println()
				output.F.Info("RepoSwarm saves investigation results to an architecture hub repo.")
				output.F.Info("You'll need a GitHub token and a target repo.")
				fmt.Println()

				fmt.Print("  Configure git provider + arch-hub now? [Y/n] ")
				gitAnswer, _ := reader.ReadString('\n')
				gitAnswer = strings.TrimSpace(gitAnswer)

				if gitAnswer == "" || strings.ToLower(gitAnswer) == "y" || strings.ToLower(gitAnswer) == "yes" {
					setupArchHub(reader, cliCfg)
				} else {
					output.F.Info("Skipped. Configure later:")
					output.F.Info("  reposwarm config worker-env set GITHUB_TOKEN <token>")
					output.F.Info("  reposwarm config worker-env set ARCH_HUB_BASE_URL https://github.com/YOUR-ORG")
					output.F.Info("  reposwarm config worker-env set ARCH_HUB_REPO_NAME <repo-name>")
				}

				// ── Post-setup: Health check ──
				fmt.Println()
				output.F.Section("Health Check")
				fmt.Println()
				output.F.Info("Running diagnostics...")
				fmt.Println()

				doctorCmd := newDoctorCmd("")
				doctorCmd.SetArgs([]string{})
				_ = doctorCmd.Execute()

				// ── Post-setup: Remote access hint ──
				fmt.Println()
				output.F.Section("Remote Access")
				fmt.Println()
				output.F.Info("To access the UI from your local browser, set up an SSH tunnel:")
				fmt.Printf("  Run: %s\n\n", output.Cyan("reposwarm tunnel"))

				_ = result // used by JSON/agent modes above
				return nil
			}

			// JSON mode — generate guides
			if flagJSON {
				cliCfgGuide, _ := config.Load()
				guideCfg := cfgToBootstrap(cliCfgGuide, env)
				guideContent := bootstrap.GenerateGuide(env, dir, guideCfg)
				agentGuideContent := bootstrap.GenerateAgentGuide(env, dir, guideCfg)

				if err := writeGuidesSilent(dir, guideContent, agentGuideContent); err != nil {
					return err
				}

				return output.JSON(map[string]any{
					"environment":    env,
					"installDir":     dir,
					"missing":        missing,
					"agentAvailable": env.AgentName() != "",
					"agent":          env.AgentName(),
					"guidePath":      filepath.Join(dir, "INSTALL.md"),
					"agentGuidePath": filepath.Join(dir, "REPOSWARM_INSTALL.md"),
				})
			}

			// Interactive mode
			fmt.Printf("\n%s\n\n", output.Bold("🚀 RepoSwarm New Installation"))
			fmt.Println(output.Dim("  Scanning environment..."))
			fmt.Println()
			fmt.Println(env.Summary())

			if len(missing) > 0 {
				fmt.Printf("\n  %s Missing dependencies:\n", output.Yellow("⚠"))
				for _, dep := range missing {
					output.F.Printf("  %s: missing\n", dep)
				}
				fmt.Println()
			}

			// Generate guides
			cliCfgGuide2, _ := config.Load()
			guideCfg2 := cfgToBootstrap(cliCfgGuide2, env)
			guideContent := bootstrap.GenerateGuide(env, dir, guideCfg2)
			agentGuideContent := bootstrap.GenerateAgentGuide(env, dir, guideCfg2)

			if guideOnly {
				return writeGuides(dir, guideContent, agentGuideContent)
			}

			if err := writeGuides(dir, guideContent, agentGuideContent); err != nil {
				return err
			}

			// Check for coding agent
			agent := env.AgentName()
			if agent != "" && !agentMode {
				fmt.Printf("\n  %s detected! Use it for interactive installation? [Y/n] ",
					output.Bold(agentDisplayName(agent)))
				reader := bufio.NewReader(os.Stdin)
				line, _ := reader.ReadString('\n')
				line = strings.TrimSpace(strings.ToLower(line))
				if line == "" || line == "y" || line == "yes" {
					agentMode = true
				}
			}

			if agentMode && agent != "" {
				return launchAgent(agent, dir)
			}

			// No agent — show manual instructions
			fmt.Printf("\n  %s\n\n", output.Bold("Next steps:"))
			fmt.Printf("  1. Review the guide:     %s\n", output.Cyan(filepath.Join(dir, "INSTALL.md")))
			fmt.Printf("  2. Follow the steps to start each service\n")
			fmt.Printf("  3. Configure the CLI:    %s\n", output.Cyan("reposwarm config set apiUrl http://localhost:<port>/v1"))
			fmt.Printf("  4. Verify:               %s\n", output.Cyan("reposwarm status"))
			fmt.Printf("\n  Or use automated setup:  %s\n", output.Cyan("reposwarm new --local"))

			if agent != "" {
				fmt.Printf("\n  Or let %s do it:\n", output.Bold(agentDisplayName(agent)))
				switch agent {
				case "claude":
					fmt.Printf("    %s\n", output.Cyan(fmt.Sprintf("cd %s && claude \"Follow REPOSWARM_INSTALL.md step by step\"", dir)))
				case "codex":
					fmt.Printf("    %s\n", output.Cyan(fmt.Sprintf("cd %s && codex \"Follow REPOSWARM_INSTALL.md step by step\"", dir)))
				case "aider":
					fmt.Printf("    %s\n", output.Cyan(fmt.Sprintf("cd %s && aider --read REPOSWARM_INSTALL.md", dir)))
				}
			}

			fmt.Println()
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Installation directory (default: ~/.reposwarm)")
	cmd.Flags().BoolVar(&agentMode, "agent", false, "Auto-launch coding agent for installation")
	cmd.Flags().BoolVar(&forceMode, "force", false, "Destroy existing install without prompting")
	cmd.Flags().BoolVar(&guideOnly, "guide-only", false, "Only generate guide files, don't prompt")
	cmd.Flags().BoolVar(&localMode, "local", false, "Automated local setup: start Temporal, API, Worker, and UI")
	return cmd
}

// fmtPrinter implements bootstrap.Printer using the output formatter.
type fmtPrinter struct{}

func (p *fmtPrinter) Section(title string) { output.F.Section(title) }
func (p *fmtPrinter) Info(msg string)      { output.F.Info(msg) }
func (p *fmtPrinter) Success(msg string)   { output.F.Success(msg) }
func (p *fmtPrinter) Warning(msg string)   { output.F.Warning(msg) }
func (p *fmtPrinter) Error(msg string)     { output.F.Error(msg) }
func (p *fmtPrinter) Printf(format string, args ...any) {
	output.F.Printf(format, args...)
}

// jsonPrinter is a no-op printer for JSON mode (output comes from the result struct).
type jsonPrinter struct{}

func (p *jsonPrinter) Section(string)              {}
func (p *jsonPrinter) Info(string)                 {}
func (p *jsonPrinter) Success(string)              {}
func (p *jsonPrinter) Warning(string)              {}
func (p *jsonPrinter) Error(string)                {}
func (p *jsonPrinter) Printf(string, ...any)       {}

func writeGuidesSilent(dir, guide, agentGuide string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	installPath := filepath.Join(dir, "INSTALL.md")
	if err := os.WriteFile(installPath, []byte(guide), 0644); err != nil {
		return fmt.Errorf("writing INSTALL.md: %w", err)
	}
	agentPath := filepath.Join(dir, "REPOSWARM_INSTALL.md")
	if err := os.WriteFile(agentPath, []byte(agentGuide), 0644); err != nil {
		return fmt.Errorf("writing REPOSWARM_INSTALL.md: %w", err)
	}
	return nil
}

func writeGuides(dir, guide, agentGuide string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	installPath := filepath.Join(dir, "INSTALL.md")
	if err := os.WriteFile(installPath, []byte(guide), 0644); err != nil {
		return fmt.Errorf("writing INSTALL.md: %w", err)
	}
	output.Successf("Generated %s", installPath)

	agentPath := filepath.Join(dir, "REPOSWARM_INSTALL.md")
	if err := os.WriteFile(agentPath, []byte(agentGuide), 0644); err != nil {
		return fmt.Errorf("writing REPOSWARM_INSTALL.md: %w", err)
	}
	output.Successf("Generated %s (agent-friendly)", agentPath)

	return nil
}

func launchAgent(agent, dir string) error {
	guidePath := filepath.Join(dir, "REPOSWARM_INSTALL.md")

	fmt.Printf("\n  %s Launching %s...\n\n",
		output.Bold("🤖"), output.Bold(agentDisplayName(agent)))

	var cmd *exec.Cmd
	switch agent {
	case "claude":
		cmd = exec.Command("claude",
			"--print",
			fmt.Sprintf("Read %s and follow every step. Install RepoSwarm in %s. Verify each step before moving to the next.", guidePath, dir))
		cmd.Dir = dir
	case "codex":
		cmd = exec.Command("codex",
			fmt.Sprintf("Follow the instructions in REPOSWARM_INSTALL.md step by step to install RepoSwarm locally in %s", dir))
		cmd.Dir = dir
	case "aider":
		cmd = exec.Command("aider", "--read", guidePath)
		cmd.Dir = dir
	default:
		return fmt.Errorf("unsupported agent: %s", agent)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("agent exited with error: %w", err)
	}

	fmt.Printf("\n  %s Agent finished. Verify with: %s\n\n",
		"Done!", "reposwarm status")
	return nil
}

func agentDisplayName(agent string) string {
	names := map[string]string{
		"claude": "Claude Code",
		"codex":  "Codex",
		"cursor": "Cursor",
		"aider":  "Aider",
	}
	if n, ok := names[agent]; ok {
		return n
	}
	return agent
}

func cfgToBootstrap(cliCfg *config.Config, env *bootstrap.Environment) *bootstrap.Config {
	cfg := &bootstrap.Config{
		WorkerRepoURL:  cliCfg.EffectiveWorkerRepoURL(),
		APIRepoURL:     cliCfg.EffectiveAPIRepoURL(),
		UIRepoURL:      cliCfg.EffectiveUIRepoURL(),
		DynamoDBTable:  cliCfg.EffectiveDynamoDBTable(),
		DefaultModel:   cliCfg.EffectiveModel(),
		TemporalPort:   cliCfg.EffectiveTemporalPort(),
		TemporalUIPort: cliCfg.EffectiveTemporalUIPort(),
		APIPort:        cliCfg.EffectiveAPIPort(),
		UIPort:         cliCfg.EffectiveUIPort(),
		Region:         cliCfg.Region,
	}
	if cfg.Region == "" {
		cfg.Region = env.AWSRegion
	}
	return cfg
}

// detectExistingInstall checks for an existing local installation and prompts
// the user on what to do. Returns true if we should abort (user chose to keep existing).
func detectExistingInstall(dir string, jsonMode, agentMode, forceMode bool) bool {
	// Check if the install directory exists with content
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return false // No existing install
	}

	// Check if services are actually running
	temporalUp := isServiceUp("http://localhost:8233/api/v1/namespaces")
	apiUp := isServiceUp("http://localhost:3000/v1/health")

	if !temporalUp && !apiUp {
		// Directory exists but nothing is running — might be leftover from a failed install
		// Continue without prompting (setupLocal handles existing dirs gracefully)
		return false
	}

	// Services are running!
	if forceMode {
		if !jsonMode && !agentMode {
			output.F.Info("Existing installation found — destroying (--force)")
		}
		teardownExisting(dir)
		return false // Continue with fresh install
	}

	if jsonMode {
		output.JSON(map[string]any{
			"error":      "existing_install",
			"message":    "A local RepoSwarm installation is already running",
			"installDir": dir,
			"temporal":   temporalUp,
			"api":        apiUp,
		})
		return true
	}

	if agentMode {
		fmt.Fprintf(os.Stderr, "A local RepoSwarm installation is already running at %s\n", dir)
		fmt.Fprintln(os.Stderr, "Use --force to destroy and reinstall, or manage with: reposwarm status")
		return true
	}

	// Interactive prompt
	output.F.Section("Existing Installation Detected")
	output.F.Success("RepoSwarm is already running!")
	if temporalUp {
		output.F.Info("  Temporal: ✓ running")
	}
	if apiUp {
		output.F.Info("  API:      ✓ running")
	}
	fmt.Println()
	fmt.Println("  What would you like to do?")
	fmt.Println()
	fmt.Printf("  %s  Destroy existing install and start fresh\n", output.Bold("[d]"))
	fmt.Printf("  %s  Keep existing install (do nothing)\n", output.Bold("[k]"))
	fmt.Println()
	fmt.Print("  Your choice (d/k): ")

	var choice string
	fmt.Scanln(&choice)

	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "d", "destroy":
		output.F.Info("Tearing down existing installation...")
		teardownExisting(dir)
		return false // Continue with fresh install
	default:
		output.F.Success("Keeping existing installation. Use 'reposwarm status' to check health.")
		return true // Abort
	}
}

func isServiceUp(url string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

func teardownExisting(dir string) {
	// Stop docker compose (handles both old "temporal" and new "reposwarm" project names)
	temporalDir := filepath.Join(dir, config.ComposeSubDir)
	if _, err := os.Stat(filepath.Join(temporalDir, "docker-compose.yml")); err == nil {
		// Try new project name first
		cmd := exec.Command("docker", "compose", "down", "-v")
		cmd.Dir = temporalDir
		cmd.Run()
		// Also clean up old "temporal" project containers if they exist
		bootstrap.CleanupOldProjectContainers()
		output.F.Info("  Containers stopped")
	}

	// Kill processes from PID files
	for _, sub := range []string{"api", "worker", "ui"} {
		pidFile := filepath.Join(dir, sub, sub+".pid")
		data, err := os.ReadFile(pidFile)
		if err != nil {
			continue
		}
		pid := strings.TrimSpace(string(data))
		killCmd := exec.Command("kill", pid)
		if killCmd.Run() == nil {
			output.F.Info(fmt.Sprintf("  %s process stopped (PID %s)", sub, pid))
		}
	}

	// Remove the directory
	os.RemoveAll(dir)
	output.F.Info("  Install directory removed")
}

// showPlanAndConfirm displays the full setup plan and asks for confirmation.
// Returns true if user wants to proceed.
func showPlanAndConfirm(plan *bootstrap.Plan, env *bootstrap.Environment, force bool) bool {
	fmt.Println()
	fmt.Printf("  %s\n", output.Bold("🚀 RepoSwarm Local Setup"))
	fmt.Println()
	fmt.Printf("  %s\n", output.Dim("Here's what we're going to set up on your machine:"))
	fmt.Println()

	// Steps
	fmt.Printf("  %s\n", output.Bold("📋 The Plan"))
	fmt.Println()
	steps := plan.Steps()
	for i, step := range steps {
		fmt.Printf("     %s  %s\n", output.Cyan(fmt.Sprintf("%d.", i+1)), step)
	}
	fmt.Println()

	// Ports
	fmt.Printf("  %s\n", output.Bold("🔌 Ports"))
	fmt.Println()
	for _, p := range plan.Ports() {
		fmt.Printf("     %-20s → localhost:%s\n", output.Dim(p[0]), output.Cyan(p[1]))
	}
	fmt.Println()

	// What it needs
	fmt.Printf("  %s\n", output.Bold("📦 What's included"))
	fmt.Println()
	fmt.Printf("     • 7 Docker containers  %s\n", output.Dim("(all services run as containers)"))
	fmt.Printf("     • API server           %s\n", output.Dim("(ghcr.io/reposwarm/api)"))
	fmt.Printf("     • Worker               %s\n", output.Dim("(ghcr.io/reposwarm/worker)"))
	fmt.Printf("     • Web UI               %s\n", output.Dim("(ghcr.io/reposwarm/ui)"))
	fmt.Printf("     • Temporal + PostgreSQL %s\n", output.Dim("(workflow engine + database)"))
	fmt.Printf("     • Temporal UI          %s\n", output.Dim("(workflow dashboard)"))
	fmt.Printf("     • DynamoDB Local       %s\n", output.Dim("(cache store)"))
	fmt.Println()

	// Timing note
	fmt.Printf("  %s %s\n", output.Yellow("⏱"), output.Dim("First run takes 3-5 minutes (Docker image pulls + Temporal schema setup)."))
	fmt.Printf("    %s\n", output.Dim("Subsequent runs start in seconds."))
	fmt.Println()

	if force {
		return true
	}

	// Confirm
	fmt.Printf("  %s ", output.Bold("Ready to go? [Y/n]"))
	var choice string
	fmt.Scanln(&choice)
	choice = strings.ToLower(strings.TrimSpace(choice))
	return choice == "" || choice == "y" || choice == "yes"
}

// spinnerPrinter implements bootstrap.Printer with animated spinners.
type spinnerPrinter struct {
	current *output.Spinner
}

func (p *spinnerPrinter) Section(title string) {
	// Stop any running spinner
	if p.current != nil {
		p.current.Stop()
		p.current = nil
	}
	fmt.Println()
	fmt.Printf("  %s\n", output.Bold(title))
	fmt.Println()
}

func (p *spinnerPrinter) Info(msg string) {
	// Start a new spinner for this step
	if p.current != nil {
		p.current.Stop()
	}
	p.current = output.NewSpinner(msg)
}

func (p *spinnerPrinter) Success(msg string) {
	if p.current != nil {
		p.current.StopSuccess(msg)
		p.current = nil
	} else {
		fmt.Printf("  %s %s\n", output.Green("✓"), msg)
	}
}

func (p *spinnerPrinter) Warning(msg string) {
	if p.current != nil {
		p.current.StopWarning(msg)
		p.current = nil
	} else {
		fmt.Printf("  %s %s\n", output.Yellow("⚠"), msg)
	}
}

func (p *spinnerPrinter) Error(msg string) {
	if p.current != nil {
		p.current.StopError(msg)
		p.current = nil
	} else {
		fmt.Fprintf(os.Stderr, "  %s %s\n", output.Red("✗"), msg)
	}
}

func (p *spinnerPrinter) Printf(format string, args ...any) {
	if p.current != nil {
		p.current.Stop()
		p.current = nil
	}
	fmt.Printf(format, args...)
}

func setupArchHub(reader *bufio.Reader, cfg *config.Config) {
	composeDir := filepath.Join(cfg.EffectiveInstallDir(), bootstrap.ComposeSubDir)
	envPath := filepath.Join(composeDir, "worker.env")

	// 1. GitHub token
	existingEnv, _ := bootstrap.ReadWorkerEnvFile(cfg.EffectiveInstallDir())
	existingToken := existingEnv["GITHUB_TOKEN"]

	if existingToken != "" {
		masked := existingToken
		if len(masked) > 8 {
			masked = masked[:4] + "..." + masked[len(masked)-4:]
		}
		fmt.Printf("  GITHUB_TOKEN already set (%s)\n", masked)
	} else {
		fmt.Print("  GitHub token (for repo access): ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		if token != "" {
			existingEnv["GITHUB_TOKEN"] = token
			output.Successf("GITHUB_TOKEN set")
		} else {
			output.F.Warning("No token provided — private repos won't be accessible")
		}
	}

	// 2. Arch-hub base URL
	fmt.Println()
	output.F.Info("Architecture hub = a git repo where .arch.md files are saved.")
	output.F.Info("Example: https://github.com/your-org/architecture-hub")
	fmt.Println()

	existingBase := existingEnv["ARCH_HUB_BASE_URL"]
	if existingBase != "" && existingBase != "https://github.com/your-org" {
		fmt.Printf("  ARCH_HUB_BASE_URL already set: %s\n", existingBase)
		fmt.Print("  Change it? [y/N] ")
		change, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(change)) != "y" {
			goto repoName
		}
	}

	{
		fmt.Print("  GitHub org/user URL (e.g. https://github.com/my-org): ")
		baseURL, _ := reader.ReadString('\n')
		baseURL = strings.TrimSpace(baseURL)
		if baseURL != "" {
			existingEnv["ARCH_HUB_BASE_URL"] = baseURL
			existingBase = baseURL
		}
	}

repoName:
	// 3. Arch-hub repo name
	existingRepo := existingEnv["ARCH_HUB_REPO_NAME"]
	defaultRepo := "architecture-hub"
	if existingRepo != "" {
		defaultRepo = existingRepo
	}

	fmt.Printf("  Arch-hub repo name [%s]: ", defaultRepo)
	repoName, _ := reader.ReadString('\n')
	repoName = strings.TrimSpace(repoName)
	if repoName == "" {
		repoName = defaultRepo
	}
	existingEnv["ARCH_HUB_REPO_NAME"] = repoName

	// 4. Write to worker.env
	var lines []string
	keys := make([]string, 0, len(existingEnv))
	for k := range existingEnv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", k, existingEnv[k]))
	}
	if err := os.WriteFile(envPath, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		output.F.Warning(fmt.Sprintf("Could not write worker.env: %v", err))
		return
	}

	// 5. Test arch-hub access
	if existingBase != "" && existingBase != "https://github.com/your-org" {
		fmt.Println()
		output.F.Info("Testing arch-hub access...")
		fullRepo := strings.TrimSuffix(existingBase, "/") + "/" + repoName
		ownerRepo := ""
		if strings.Contains(fullRepo, "github.com/") {
			parts := strings.SplitN(strings.TrimPrefix(fullRepo, "https://github.com/"), "/", 3)
			if len(parts) >= 2 {
				ownerRepo = parts[0] + "/" + parts[1]
			}
		}

		if ownerRepo != "" {
			token := existingEnv["GITHUB_TOKEN"]
			client := &http.Client{Timeout: 10 * time.Second}
			apiURL := fmt.Sprintf("https://api.github.com/repos/%s", ownerRepo)
			req, _ := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
			if req != nil && token != "" {
				req.Header.Set("Authorization", "token "+token)
			}
			if req != nil {
				resp, err := client.Do(req)
				if err != nil {
					output.F.Warning(fmt.Sprintf("Cannot reach %s: %v", ownerRepo, err))
				} else {
					resp.Body.Close()
					switch {
					case resp.StatusCode == 200:
						output.Successf("Arch-hub '%s' is accessible ✅", ownerRepo)
					case resp.StatusCode == 404:
						output.F.Warning(fmt.Sprintf("Repo '%s' not found", ownerRepo))
						fmt.Println()
						fmt.Print("  Create it now? [Y/n] ")
						create, _ := reader.ReadString('\n')
						create = strings.TrimSpace(create)
						if create == "" || strings.ToLower(create) == "y" {
							createArchHubRepo(ownerRepo, token)
						}
					case resp.StatusCode == 401 || resp.StatusCode == 403:
						output.F.Warning(fmt.Sprintf("Auth failed for '%s' (HTTP %d) — check GITHUB_TOKEN", ownerRepo, resp.StatusCode))
					default:
						output.F.Warning(fmt.Sprintf("Unexpected HTTP %d for '%s'", resp.StatusCode, ownerRepo))
					}
				}
			}
		}
	}

	fmt.Println()
	output.Successf("Arch-hub configured: %s/%s", existingBase, repoName)
	output.F.Warning("Restart worker to apply: reposwarm restart worker")
}

func createArchHubRepo(ownerRepo string, token string) {
	parts := strings.SplitN(ownerRepo, "/", 2)
	if len(parts) != 2 || token == "" {
		output.F.Warning("Cannot create repo (need owner/name and GITHUB_TOKEN)")
		return
	}

	org, name := parts[0], parts[1]

	// Try org repo first, fall back to user repo
	client := &http.Client{Timeout: 15 * time.Second}
	body := fmt.Sprintf(`{"name":"%s","description":"Architecture documentation generated by RepoSwarm","private":true,"auto_init":true}`, name)

	// Try as org repo
	apiURL := fmt.Sprintf("https://api.github.com/orgs/%s/repos", org)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", apiURL, strings.NewReader(body))
	if req != nil {
		req.Header.Set("Authorization", "token "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 201 {
				output.Successf("Created private repo '%s' ✅", ownerRepo)
				return
			}
		}
	}

	// Fall back to user repo
	apiURL = "https://api.github.com/user/repos"
	req, _ = http.NewRequestWithContext(context.Background(), "POST", apiURL, strings.NewReader(body))
	if req != nil {
		req.Header.Set("Authorization", "token "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 201 {
				output.Successf("Created private repo '%s' ✅", ownerRepo)
				return
			}
			output.F.Warning(fmt.Sprintf("Could not create repo (HTTP %d) — create it manually on GitHub", resp.StatusCode))
		} else {
			output.F.Warning(fmt.Sprintf("Failed to create repo: %v", err))
		}
	}
}
