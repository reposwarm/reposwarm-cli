package commands

import (
	"fmt"
	"os"
	"sort"
	"sync/atomic"
	"strings"
	"time"

	"github.com/reposwarm/reposwarm-cli/internal/api"
	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

// investigationSteps defines the ordered list of investigation steps with labels.
var investigationSteps = []struct {
	ID    string
	Label string
}{
	{"hl_overview", "Overview"},
	{"module_deep_dive", "Module Deep Dive"},
	{"core_entities", "Core Entities"},
	{"data_mapping", "Data Mapping"},
	{"DBs", "Databases"},
	{"APIs", "APIs"},
	{"events", "Events"},
	{"dependencies", "Dependencies"},
	{"service_dependencies", "Service Dependencies"},
	{"authentication", "Authentication"},
	{"authorization", "Authorization"},
	{"security_check", "Security"},
	{"prompt_security_check", "Prompt Security"},
	{"deployment", "Deployment"},
	{"monitoring", "Monitoring"},
	{"ml_services", "ML Services"},
	{"feature_flags", "Feature Flags"},
}

func newWorkflowsWatchRepoCmd() *cobra.Command {
	var wait bool
	var repo string

	cmd := &cobra.Command{
		Use:   "progress",
		Short: "Show progress of active investigations",
		Long: `Shows a summary of active investigations (daily batch or individual).
Displays completed, in-progress, and pending steps.

Use --repo to track a specific repo's investigation with a live progress bar.
Add --wait to keep watching until the investigation finishes.`,
		Args: friendlyMaxArgs(0, `reposwarm workflows progress [--repo <name>] [--wait]

Examples:
  reposwarm wf progress
  reposwarm wf progress --repo is-odd
  reposwarm wf progress --repo is-odd --wait`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if repo == "" {
				// No --repo: fall back to the original overview progress
				return showOverviewProgress()
			}
			return showRepoProgress(repo, wait)
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "Track a specific repo's investigation")
	cmd.Flags().BoolVar(&wait, "wait", false, "Keep watching until the investigation finishes")
	return cmd
}

func showRepoProgress(repoName string, wait bool) error {
	client, err := getClient()
	if err != nil {
		return err
	}

	// Get configured model
	cliCfg, _ := config.Load()
	model := cliCfg.EffectiveModel()

	// Find the workflow for this repo
	workflowID, err := findRepoWorkflow(client, repoName)
	if err != nil {
		return err
	}

	if workflowID == "" {
		if flagJSON {
			return output.JSON(map[string]any{"error": "no_workflow", "repo": repoName})
		}
		output.Infof("No active investigation found for '%s'", repoName)
		fmt.Println()
		fmt.Printf("  💡 Start one with: %s\n\n", output.Cyan(fmt.Sprintf("reposwarm investigate %s", repoName)))
		return nil
	}

	if !wait {
		return showRepoSnapshot(client, repoName, workflowID, model)
	}

	// --wait mode: poll until done
	return watchRepoUntilDone(client, repoName, workflowID, model)
}

func findRepoWorkflow(client *api.Client, repoName string) (string, error) {
	var result api.WorkflowsResponse
	if err := client.Get(ctx(), "/workflows?pageSize=100", &result); err != nil {
		return "", err
	}

	// Find the most recent running workflow for this repo
	prefix := fmt.Sprintf("investigate-single-%s-", repoName)
	var best *api.WorkflowExecution
	for i, w := range result.Executions {
		if strings.HasPrefix(w.WorkflowID, prefix) {
			if best == nil || w.StartTime > best.StartTime {
				best = &result.Executions[i]
			}
		}
	}

	if best != nil {
		return best.WorkflowID, nil
	}
	return "", nil
}

func getCompletedSteps(client *api.Client, repoName string) (map[string]bool, error) {
	var index api.WikiIndex
	if err := client.Get(ctx(), fmt.Sprintf("/wiki/%s", repoName), &index); err != nil {
		// Wiki might not exist yet
		return map[string]bool{}, nil
	}

	completed := make(map[string]bool)
	for _, s := range index.Sections {
		completed[s.Name()] = true
	}
	return completed, nil
}

func getWorkflowStatus(client *api.Client, workflowID string) (string, string, error) {
	var wf api.WorkflowExecution
	if err := client.Get(ctx(), fmt.Sprintf("/workflows/%s", workflowID), &wf); err != nil {
		return "", "", err
	}
	return wf.Status, wf.StartTime, nil
}

func showRepoSnapshot(client *api.Client, repoName, workflowID, model string) error {
	status, startTime, err := getWorkflowStatus(client, workflowID)
	if err != nil {
		return err
	}

	completed, err := getCompletedSteps(client, repoName)
	if err != nil {
		return err
	}

	total := len(investigationSteps)
	done := 0
	for _, step := range investigationSteps {
		if completed[step.ID] {
			done++
		}
	}

	// Find the current step (first incomplete one)
	currentStep := ""
	for _, step := range investigationSteps {
		if !completed[step.ID] {
			currentStep = step.Label
			break
		}
	}

	if flagJSON {
		steps := make([]map[string]any, len(investigationSteps))
		for i, step := range investigationSteps {
			s := "pending"
			if completed[step.ID] {
				s = "done"
			} else if step.Label == currentStep {
				s = "active"
			}
			steps[i] = map[string]any{"id": step.ID, "label": step.Label, "status": s}
		}
		return output.JSON(map[string]any{
			"repo":       repoName,
			"workflowId": workflowID,
			"status":     status,
			"startTime":  startTime,
			"completed":  done,
			"total":      total,
			"current":    currentStep,
			"model":      model,
			"steps":      steps,
		})
	}

	if flagAgent {
		renderProgressDisplay(repoName, workflowID, status, startTime, completed, done, total, currentStep, model)
		return nil
	}

	// Human mode: live view with 'q' to quit
	oldState, rawErr := makeRaw(stdinFd())
	if rawErr == nil {
		defer restoreTerminal(stdinFd(), oldState)
	}

	var quit atomic.Bool
	if rawErr == nil {
		go func() {
			buf := make([]byte, 1)
			for {
				n, err := os.Stdin.Read(buf)
				if err != nil || n == 0 {
					continue
				}
				if buf[0] == 'q' || buf[0] == 'Q' || buf[0] == 3 {
					quit.Store(true)
					return
				}
			}
		}()
	}

	clearScreen()
	renderProgressDisplay(repoName, workflowID, status, startTime, completed, done, total, currentStep, model)
	fmt.Printf("\n  %s\n", output.Dim("Press q to quit"))

	// Wait for 'q'
	for {
		if quit.Load() {
			clearScreen()
			fmt.Print("\n  👋 Closed.\n\n")
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func watchRepoUntilDone(client *api.Client, repoName, workflowID, model string) error {
	if flagJSON {
		return watchRepoJSON(client, repoName, workflowID, model)
	}
	return watchRepoHuman(client, repoName, workflowID, model)
}

func watchRepoJSON(client *api.Client, repoName, workflowID, model string) error {
	seen := map[string]bool{}
	for {
		status, _, err := getWorkflowStatus(client, workflowID)
		if err != nil {
			return err
		}

		completed, _ := getCompletedSteps(client, repoName)
		for _, step := range investigationSteps {
			if completed[step.ID] && !seen[step.ID] {
				seen[step.ID] = true
				output.JSON(map[string]any{
					"event":     "step_complete",
					"repo":      repoName,
					"step":      step.ID,
					"label":     step.Label,
					"completed": len(seen),
					"total":     len(investigationSteps),
				})
			}
		}

		if status != "Running" {
			output.JSON(map[string]any{
				"event":     "workflow_done",
					"model":      model,
				"repo":      repoName,
				"status":    status,
				"completed": len(seen),
				"total":     len(investigationSteps),
			})
			return nil
		}
		time.Sleep(3 * time.Second)
	}
}

func watchRepoHuman(client *api.Client, repoName, workflowID, model string) error {
	total := len(investigationSteps)
	lastDone := -1

	// Set up 'q' to quit
	oldState, rawErr := makeRaw(stdinFd())
	if rawErr == nil {
		defer restoreTerminal(stdinFd(), oldState)
	}

	var quit atomic.Bool
	if rawErr == nil {
		go func() {
			buf := make([]byte, 1)
			for {
				n, err := os.Stdin.Read(buf)
				if err != nil || n == 0 {
					continue
				}
				if buf[0] == 'q' || buf[0] == 'Q' || buf[0] == 3 {
					quit.Store(true)
					return
				}
			}
		}()
	}

	for {
		if quit.Load() {
			clearScreen()
			fmt.Print("\n  👋 Closed.\n\n")
			return nil
		}
		status, startTime, err := getWorkflowStatus(client, workflowID)
		if err != nil {
			return err
		}

		completed, _ := getCompletedSteps(client, repoName)
		done := 0
		for _, step := range investigationSteps {
			if completed[step.ID] {
				done++
			}
		}

		currentStep := ""
		for _, step := range investigationSteps {
			if !completed[step.ID] {
				currentStep = step.Label
				break
			}
		}

		if done != lastDone || status != "Running" {
			// Clear screen and redraw
			fmt.Print("\033[H\033[2J")
			renderProgressDisplay(repoName, workflowID, status, startTime, completed, done, total, currentStep, model)
			fmt.Printf("\n  %s\n", output.Dim("Press q to quit · refreshes every 3s"))
			lastDone = done
		}

		if status != "Running" {
			fmt.Println()
			if status == "Completed" {
				fmt.Printf("  🎉 %s\n\n", output.Green("Investigation complete!"))
				fmt.Printf("  View results: %s\n", output.Cyan(fmt.Sprintf("reposwarm results read %s", repoName)))
				fmt.Printf("  Full report:  %s\n\n", output.Cyan(fmt.Sprintf("reposwarm results read %s --all", repoName)))
			} else {
				fmt.Printf("  %s Investigation ended with status: %s\n\n", output.Red("✗"), status)
			}
			return nil
		}

		time.Sleep(3 * time.Second)
	}
}

func renderProgressDisplay(repoName, workflowID, status, startTime string, completed map[string]bool, done, total int, currentStep, model string) {
	fmt.Println()
	fmt.Printf("  %s\n\n", output.Bold(fmt.Sprintf("🔍 Investigating: %s", repoName)))

	// Status line
	fmt.Printf("  %-14s %s\n", output.Dim("Workflow:"), output.Dim(workflowID))
	fmt.Printf("  %-14s %s\n", output.Dim("Status:"), output.StatusColor(status))
	if startTime != "" {
		elapsedStr := elapsed(startTime)
		// Staleness warning if running with zero progress for >30min
		if done == 0 && status == "RUNNING" {
			if mins := elapsedMinutes(startTime); mins > 30 {
				elapsedStr += "  " + output.Yellow(fmt.Sprintf("⚠ no progress in %dm — run: reposwarm errors", mins))
			}
		} else if done > 0 && done < total && status == "RUNNING" {
			// Warn if stuck on same step for >20min (rough heuristic based on total time vs progress)
			if mins := elapsedMinutes(startTime); mins > 0 {
				avgPerStep := mins / done
				if avgPerStep > 20 {
					elapsedStr += "  " + output.Yellow("⚠ slower than expected")
				}
			}
		}
		fmt.Printf("  %-14s %s\n", output.Dim("Elapsed:"), elapsedStr)
	}
	if model != "" {
		fmt.Printf("  %-14s %s\n", output.Dim("Model:"), model)
	}
	fmt.Println()

	// Progress bar
	barWidth := 40
	filled := 0
	if total > 0 {
		filled = barWidth * done / total
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	pct := 0
	if total > 0 {
		pct = done * 100 / total
	}
	fmt.Printf("  %s %d%% (%d/%d)\n\n", bar, pct, done, total)

	// Step checklist
	for _, step := range investigationSteps {
		if completed[step.ID] {
			fmt.Printf("  %s  %s\n", output.Green("✓"), step.Label)
		} else if step.Label == currentStep && status == "Running" {
			fmt.Printf("  %s  %s %s\n", output.Cyan("⠹"), output.Bold(step.Label), output.Dim("← active"))
		} else {
			fmt.Printf("  %s  %s\n", output.Dim("○"), output.Dim(step.Label))
		}
	}
	fmt.Println()
}

// showOverviewProgress is the original progress behavior (batch + standalone).
func showOverviewProgress() error {
	client, err := getClient()
	if err != nil {
		return err
	}

	var result api.WorkflowsResponse
	if err := client.Get(ctx(), "/workflows?pageSize=100", &result); err != nil {
		return err
	}

	// Find active workflows — daily batch OR standalone single-repo
	var daily *api.WorkflowExecution
	for i, w := range result.Executions {
		if w.Type == "InvestigateReposWorkflow" && w.Status == "Running" {
			daily = &result.Executions[i]
			break
		}
	}

	var children []api.WorkflowExecution
	if daily != nil {
		for _, w := range result.Executions {
			if w.Type == "InvestigateSingleRepoWorkflow" && w.StartTime >= daily.StartTime {
				children = append(children, w)
			}
		}
	} else {
		var anyRunning bool
		// Find the most recently started single-repo workflow
		var latestStart string
		for _, w := range result.Executions {
			if w.Type == "InvestigateSingleRepoWorkflow" && w.Status == "Running" {
				if w.StartTime > latestStart {
					latestStart = w.StartTime
				}
			}
		}

		// Only include workflows started within 5 minutes of the latest one
		// This filters out stale workflows from previous runs
		cutoff := ""
		if latestStart != "" {
			if t, err := time.Parse(time.RFC3339Nano, latestStart); err == nil {
				cutoff = t.Add(-5 * time.Minute).Format(time.RFC3339Nano)
			} else if t, err := time.Parse("2006-01-02T15:04:05Z", latestStart); err == nil {
				cutoff = t.Add(-5 * time.Minute).Format(time.RFC3339Nano)
			}
		}

		for _, w := range result.Executions {
			if w.Type == "InvestigateSingleRepoWorkflow" {
				// Filter: only include recent workflows (within 5 min of latest)
				if cutoff != "" && w.StartTime < cutoff {
					continue
				}
				if w.Status == "Running" {
					anyRunning = true
				}
				children = append(children, w)
			}
		}
		if !anyRunning {
			if flagJSON {
				return output.JSON(map[string]any{"error": "no active investigations"})
			}
			output.Infof("No active investigations found")
			return nil
		}
	}

	var running, completedWfs, failed []api.WorkflowExecution
	for _, w := range children {
		switch w.Status {
		case "Running":
			running = append(running, w)
		case "Completed":
			completedWfs = append(completedWfs, w)
		case "Failed":
			failed = append(failed, w)
		}
	}

	sort.Slice(completedWfs, func(i, j int) bool {
		return completedWfs[i].CloseTime < completedWfs[j].CloseTime
	})
	sort.Slice(running, func(i, j int) bool {
		return running[i].WorkflowID < running[j].WorkflowID
	})

	var totalRepos int
	if daily != nil {
		var repos []api.Repository
		totalRepos = 36
		if err := client.Get(ctx(), "/repos", &repos); err == nil {
			enabled := 0
			for _, r := range repos {
				if r.Enabled {
					enabled++
				}
			}
			if enabled > 0 {
				totalRepos = enabled
			}
		}
	} else {
		totalRepos = len(children)
	}

	if flagJSON {
		dailyID := ""
		dailyStart := ""
		if daily != nil {
			dailyID = daily.WorkflowID
			dailyStart = daily.StartTime
		}
		return output.JSON(map[string]any{
			"dailyWorkflowId": dailyID,
			"startTime":       dailyStart,
			"totalRepos":      totalRepos,
			"completed":       len(completedWfs),
			"running":         len(running),
			"failed":          len(failed),
			"pending":         totalRepos - len(children),
			"completedRepos":  repoNames(completedWfs),
			"runningRepos":    repoNames(running),
			"failedRepos":     repoNames(failed),
		})
	}

	pending := totalRepos - len(children)

	title := "Investigation Progress"
	if daily != nil {
		title = "Daily Investigation Progress"
	}
	output.F.Section(title)
	if daily != nil {
		output.F.KeyValue("Workflow", daily.WorkflowID)
		output.F.KeyValue("Started", daily.StartTime[:19])
		output.F.KeyValue("Elapsed", elapsed(daily.StartTime))
	}
	output.F.Progress(len(completedWfs), totalRepos)
	output.F.Println()
	output.F.Printf("Completed: %-3d  Running: %-3d  Failed: %-3d  Pending: %-3d\n",
		len(completedWfs), len(running), len(failed), pending)
	output.F.Println()

	if len(completedWfs) > 0 {
		output.F.Section("Completed")
		for _, w := range completedWfs {
			output.F.Printf("  %-35s %s\n", repoName(w.WorkflowID), duration(w))
		}
	}

	if len(running) > 0 {
		output.F.Section("In Progress")
		for _, w := range running {
			output.F.Printf("  %-35s %s elapsed\n", repoName(w.WorkflowID), elapsed(w.StartTime))
		}
	}

	if len(failed) > 0 {
		output.F.Section("Failed")
		for _, w := range failed {
			output.F.Printf("  %-35s %s\n", repoName(w.WorkflowID), duration(w))
		}
	}

	if pending > 0 {
		output.F.Println()
		output.F.Printf("%d repos waiting to start\n", pending)
	}

	return nil
}

func repoName(workflowID string) string {
	// Handle both "investigate-single-<repo>-<ts>" and "investigate-single-repo-<repo>"
	name := workflowID
	name = strings.TrimPrefix(name, "investigate-single-repo-")
	name = strings.TrimPrefix(name, "investigate-single-")
	// Strip trailing timestamp (digits after last dash)
	if idx := strings.LastIndex(name, "-"); idx > 0 {
		suffix := name[idx+1:]
		allDigits := true
		for _, c := range suffix {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if allDigits && len(suffix) > 8 {
			name = name[:idx]
		}
	}
	return name
}

func repoNames(wfs []api.WorkflowExecution) []string {
	names := make([]string, len(wfs))
	for i, w := range wfs {
		names[i] = repoName(w.WorkflowID)
	}
	return names
}

func elapsedMinutes(startTime string) int {
	t, err := time.Parse(time.RFC3339Nano, startTime)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05Z", startTime)
		if err != nil {
			return 0
		}
	}
	return int(time.Since(t).Minutes())
}

func elapsed(startTime string) string {
	t, err := time.Parse(time.RFC3339Nano, startTime)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05Z", startTime)
		if err != nil {
			return "?"
		}
	}
	d := time.Since(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func duration(w api.WorkflowExecution) string {
	if w.CloseTime == "" {
		return elapsed(w.StartTime)
	}
	start, err1 := time.Parse(time.RFC3339Nano, w.StartTime)
	end, err2 := time.Parse(time.RFC3339Nano, w.CloseTime)
	if err1 != nil || err2 != nil {
		return "?"
	}
	d := end.Sub(start)
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}
