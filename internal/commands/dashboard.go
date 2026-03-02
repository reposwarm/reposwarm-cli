package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func newDashboardCmd() *cobra.Command {
	var interval int
	var focusRepo string

	cmd := &cobra.Command{
		Use:   "dashboard",
		Aliases: []string{"dash", "top"},
		Short: "Live dashboard of all investigations (like htop)",
		Long: `Shows a live-updating dashboard of all investigation workflows.
Sorted by progress (most complete first). Press 'q' to quit.

Examples:
  reposwarm dashboard
  reposwarm dashboard --repo is-odd
  reposwarm dash
  reposwarm top`,
		Args: friendlyMaxArgs(0, `reposwarm dashboard

No arguments needed — just run it!`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagJSON {
				return dashboardJSON()
			}
			return dashboardHuman(interval, focusRepo)
		},
	}

	cmd.Flags().IntVar(&interval, "interval", 3, "Refresh interval in seconds")
	cmd.Flags().StringVar(&focusRepo, "repo", "", "Focus on a specific repo (show step detail + errors)")
	return cmd
}

// dashRow holds one workflow's display data.
type dashRow struct {
	Repo      string
	Status    string
	Completed int
	Total     int
	Current   string
	Elapsed   string
	WorkflowID string
}

func dashboardHuman(interval int, focusRepo string) error {
	client, err := getClient()
	if err != nil {
		return err
	}

	// Set terminal to raw mode to capture 'q' without Enter
	oldState, err := makeRaw(int(os.Stdin.Fd()))
	if err != nil {
		// Fall back to non-raw mode (Ctrl+C still works)
		return dashboardLoop(client, interval, focusRepo, nil)
	}
	defer restoreTerminal(int(os.Stdin.Fd()), oldState)

	// Quit signal
	var quit atomic.Bool
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				continue
			}
			if buf[0] == 'q' || buf[0] == 'Q' || buf[0] == 3 { // q, Q, or Ctrl+C
				quit.Store(true)
				return
			}
		}
	}()

	return dashboardLoop(client, interval, focusRepo, &quit)
}

func dashboardLoop(client *api.Client, interval int, focusRepo string, quit *atomic.Bool) error {
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	// Initial render
	if err := renderDashboard(client, focusRepo); err != nil {
		return err
	}

	for {
		if quit != nil && quit.Load() {
			clearScreen()
			fmt.Print("\n  👋 Dashboard closed.\n\n")
			return nil
		}

		select {
		case <-ticker.C:
			if err := renderDashboard(client, focusRepo); err != nil {
				// Render error but keep going
				fmt.Fprintf(os.Stderr, "\r  ⚠ refresh failed: %s", err)
			}
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func renderDashboard(client *api.Client, focusRepo string) error {
	// Fetch workflows
	var result api.WorkflowsResponse
	if err := client.Get(ctx(), "/workflows?pageSize=100", &result); err != nil {
		return err
	}

	// Build rows for investigation workflows
	rows := []dashRow{}
	for _, w := range result.Executions {
		if w.Type != "InvestigateSingleRepoWorkflow" {
			continue
		}

		name := repoName(w.WorkflowID)
		total := len(investigationSteps)
		completed := 0
		currentStep := ""

		// Try to get completed steps from wiki
		var index api.WikiIndex
		if err := client.Get(ctx(), fmt.Sprintf("/wiki/%s", name), &index); err == nil {
			done := map[string]bool{}
			for _, s := range index.Sections {
				done[s.Name()] = true
			}
			for _, step := range investigationSteps {
				if done[step.ID] {
					completed++
				} else if currentStep == "" && w.Status == "Running" {
					currentStep = step.Label
				}
			}
		}

		if w.Status != "Running" && currentStep == "" {
			if w.Status == "Completed" {
				currentStep = "Done"
			} else {
				currentStep = w.Status
			}
		}

		rows = append(rows, dashRow{
			Repo:       name,
			Status:     w.Status,
			Completed:  completed,
			Total:      total,
			Current:    currentStep,
			Elapsed:    elapsed(w.StartTime),
			WorkflowID: w.WorkflowID,
		})
	}

	// Sort: running first (by most progress), then completed, then failed
	sort.Slice(rows, func(i, j int) bool {
		ri, rj := statusPriority(rows[i].Status), statusPriority(rows[j].Status)
		if ri != rj {
			return ri < rj
		}
		// Within same status group, sort by progress (desc)
		return rows[i].Completed > rows[j].Completed
	})

	// Also check for batch workflows
	var batchCount int
	for _, w := range result.Executions {
		if w.Type == "InvestigateReposWorkflow" && w.Status == "Running" {
			batchCount++
		}
	}

	// Render
	clearScreen()
	fmt.Println()
	fmt.Printf("  %s  %s\n", output.Bold("⚡ RepoSwarm Dashboard"), output.Dim(time.Now().Format("15:04:05")))
	fmt.Println()

	if len(rows) == 0 {
		fmt.Printf("  %s\n", output.Dim("No investigations found."))
		fmt.Println()
		fmt.Printf("  Start one: %s\n", output.Cyan("reposwarm investigate <repo>"))
	} else {
		// Summary line
		running, completed, failed := 0, 0, 0
		for _, r := range rows {
			switch r.Status {
			case "Running":
				running++
			case "Completed":
				completed++
			default:
				failed++
			}
		}
		fmt.Printf("  Running: %s  Completed: %s  Failed: %s",
			output.Bold(fmt.Sprintf("%d", running)),
			output.Green(fmt.Sprintf("%d", completed)),
			output.Red(fmt.Sprintf("%d", failed)))
		if batchCount > 0 {
			fmt.Printf("  %s", output.Dim(fmt.Sprintf("(%d batch)", batchCount)))
		}
		fmt.Println()
		fmt.Println()

		// Header
		fmt.Printf("  %-20s %-10s %-22s %-8s %s\n",
			output.Bold("REPO"),
			output.Bold("STATUS"),
			output.Bold("PROGRESS"),
			output.Bold("TIME"),
			output.Bold("CURRENT STEP"))
		fmt.Printf("  %s\n", output.Dim(strings.Repeat("─", 78)))

		// Rows
		for _, r := range rows {
			// Mini progress bar (12 chars)
			barWidth := 12
			filled := 0
			if r.Total > 0 {
				filled = barWidth * r.Completed / r.Total
			}
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			pct := 0
			if r.Total > 0 {
				pct = r.Completed * 100 / r.Total
			}
			progress := fmt.Sprintf("%s %2d/%d %3d%%", bar, r.Completed, r.Total, pct)

			// Color the progress bar
			if r.Status == "Completed" {
				progress = output.Green(progress)
			} else if r.Status == "Running" {
				progress = output.Cyan(progress)
			}

			// Status with color
			statusStr := output.StatusColor(r.Status)

			// Current step
			current := r.Current
			if len(current) > 20 {
				current = current[:17] + "..."
			}

			repo := r.Repo
			if len(repo) > 20 {
				repo = repo[:17] + "..."
			}

			fmt.Printf("  %-20s %-10s %s %-8s %s\n",
				repo, statusStr, progress, r.Elapsed, output.Dim(current))
		}
	}

	// If a repo is focused, show step detail + errors below the grid
	if focusRepo != "" {
		renderFocusedRepo(client, focusRepo, rows)
	}

	fmt.Println()
	fmt.Printf("  %s\n", output.Dim("Press q to quit · refreshes every 3s"))
	fmt.Println()

	return nil
}

func statusPriority(s string) int {
	switch s {
	case "Running":
		return 0
	case "Completed":
		return 1
	default:
		return 2
	}
}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

// dashboardJSON outputs a single snapshot and exits.
func dashboardJSON() error {
	client, err := getClient()
	if err != nil {
		return err
	}

	var result api.WorkflowsResponse
	if err := client.Get(ctx(), "/workflows?pageSize=100", &result); err != nil {
		return err
	}

	rows := []map[string]any{}
	for _, w := range result.Executions {
		if w.Type != "InvestigateSingleRepoWorkflow" {
			continue
		}
		name := repoName(w.WorkflowID)
		completed := 0
		total := len(investigationSteps)
		currentStep := ""

		var index api.WikiIndex
		if err := client.Get(ctx(), fmt.Sprintf("/wiki/%s", name), &index); err == nil {
			done := map[string]bool{}
			for _, s := range index.Sections {
				done[s.Name()] = true
			}
			for _, step := range investigationSteps {
				if done[step.ID] {
					completed++
				} else if currentStep == "" {
					currentStep = step.Label
				}
			}
		}

		rows = append(rows, map[string]any{
			"repo":       name,
			"workflowId": w.WorkflowID,
			"status":     w.Status,
			"completed":  completed,
			"total":      total,
			"current":    currentStep,
			"startTime":  w.StartTime,
		})
	}

	return output.JSON(rows)
}

// Raw terminal helpers using x/sys/unix
func makeRaw(fd int) (*unix.Termios, error) {
	oldState, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, err
	}
	newState := *oldState
	newState.Lflag &^= unix.ECHO | unix.ICANON
	newState.Cc[unix.VMIN] = 1
	newState.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &newState); err != nil {
		return nil, err
	}
	return oldState, nil
}

func restoreTerminal(fd int, state *unix.Termios) {
	unix.IoctlSetTermios(fd, unix.TCSETS, state)
}

// renderFocusedRepo shows step checklist + errors for a specific repo below the grid.
func renderFocusedRepo(client *api.Client, focusRepo string, rows []dashRow) {
	// Find the focused row
	var focused *dashRow
	for i, r := range rows {
		if r.Repo == focusRepo {
			focused = &rows[i]
			break
		}
	}

	if focused == nil {
		fmt.Println()
		fmt.Printf("  %s No investigation found for '%s'\n", output.Yellow("⚠"), focusRepo)
		return
	}

	// Get completed steps
	completed, _ := getCompletedSteps(client, focusRepo)

	// Step checklist
	fmt.Println()
	fmt.Printf("  %s\n", output.Bold(fmt.Sprintf("📋 %s — Steps", focusRepo)))
	fmt.Println()

	for _, step := range investigationSteps {
		if completed[step.ID] {
			fmt.Printf("    %s  %s\n", output.Green("✓"), step.Label)
		} else if step.Label == focused.Current && focused.Status == "Running" {
			fmt.Printf("    %s  %s %s\n", output.Cyan("⠹"), output.Bold(step.Label), output.Dim("← active"))
		} else {
			fmt.Printf("    %s  %s\n", output.Dim("○"), output.Dim(step.Label))
		}
	}

	// Get errors from workflow history
	errors := getWorkflowErrors(client, focused.WorkflowID)
	if len(errors) > 0 {
		fmt.Println()
		fmt.Printf("  %s\n", output.Bold(fmt.Sprintf("❌ Errors (%d)", len(errors))))
		fmt.Println()
		for _, e := range errors {
			fmt.Printf("    %s  %s\n", output.Red("✗"), e.Summary)
			if e.Detail != "" {
				// Indent detail lines
				for _, line := range strings.Split(e.Detail, "\n") {
					if line = strings.TrimSpace(line); line != "" {
						fmt.Printf("       %s\n", output.Dim(truncate(line, 80)))
					}
				}
			}
			fmt.Printf("       %s\n", output.Dim(e.Timestamp))
			fmt.Println()
		}
	}
}
