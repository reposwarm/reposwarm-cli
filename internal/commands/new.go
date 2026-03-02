package commands

import (
	"bufio"
	"net/http"
	"time"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/loki-bedlam/reposwarm-cli/internal/bootstrap"
	"github.com/loki-bedlam/reposwarm-cli/internal/config"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
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
(Temporal, API, Worker, UI) via Docker Compose and npm/pip.

Examples:
  reposwarm new                    # Interactive setup in ./reposwarm
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
					return output.JSON(result)
				}
				if flagAgent {
					printer := &fmtPrinter{}
					_, err := bootstrap.SetupLocal(env, dir, bsCfg, printer)
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
				_, err := bootstrap.SetupLocal(env, dir, bsCfg, printer)
				return err
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

	cmd.Flags().StringVar(&dir, "dir", "", "Installation directory (default: ./reposwarm)")
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
	// Stop docker compose
	temporalDir := filepath.Join(dir, "temporal")
	if _, err := os.Stat(filepath.Join(temporalDir, "docker-compose.yml")); err == nil {
		cmd := exec.Command("docker", "compose", "down", "-v")
		cmd.Dir = temporalDir
		cmd.Run()
		output.F.Info("  Temporal containers stopped")
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
	fmt.Printf("     • 3 Docker containers  %s\n", output.Dim("(Temporal + PostgreSQL + DynamoDB Local)"))
	fmt.Printf("     • API server           %s\n", output.Dim("(Node.js)"))
	fmt.Printf("     • Python worker        %s\n", output.Dim("(handles investigations)"))
	fmt.Printf("     • Web UI               %s\n", output.Dim("(Next.js dev server)"))
	fmt.Println()

	// Timing note
	fmt.Printf("  %s %s\n", output.Yellow("⏱"), output.Dim("First run takes 5-8 minutes (Docker pulls + Temporal schema setup)."))
	fmt.Printf("    %s\n", output.Dim("Subsequent runs are much faster."))
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
