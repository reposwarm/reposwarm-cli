package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newWorkflowsProgressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "progress",
		Short: "Show progress of active investigations",
		Long: `Shows a summary of active investigations (daily batch or individual).
Displays completed, in-progress, and pending repositories.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			// Fetch all workflows (up to 100)
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

			// Collect investigation workflows
			var children []api.WorkflowExecution
			if daily != nil {
				// Daily batch mode: find child workflows started after the daily
				for _, w := range result.Executions {
					if w.Type == "InvestigateSingleRepoWorkflow" && w.StartTime >= daily.StartTime {
						children = append(children, w)
					}
				}
			} else {
				// No daily batch — look for any running single-repo investigations
				var anyRunning bool
				for _, w := range result.Executions {
					if w.Type == "InvestigateSingleRepoWorkflow" {
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

			// Categorize
			var running, completed, failed []api.WorkflowExecution
			for _, w := range children {
				switch w.Status {
				case "Running":
					running = append(running, w)
				case "Completed":
					completed = append(completed, w)
				case "Failed":
					failed = append(failed, w)
				}
			}

			sort.Slice(completed, func(i, j int) bool {
				return completed[i].CloseTime < completed[j].CloseTime
			})
			sort.Slice(running, func(i, j int) bool {
				return running[i].WorkflowID < running[j].WorkflowID
			})

			// Count total repos
			var totalRepos int
			if daily != nil {
				// Daily mode: count enabled repos
				var repos []api.Repository
				totalRepos = 36 // fallback
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
				return output.JSON(map[string]any{
					"dailyWorkflowId": func() string { if daily != nil { return daily.WorkflowID }; return "" }(),
					"startTime":       func() string { if daily != nil { return daily.StartTime }; return "" }(),
					"totalRepos":      totalRepos,
					"completed":       len(completed),
					"running":         len(running),
					"failed":          len(failed),
					"pending":         totalRepos - len(children),
					"completedRepos":  repoNames(completed),
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
			output.F.Progress(len(completed), totalRepos)
			output.F.Println()
			output.F.Printf("Completed: %-3d  Running: %-3d  Failed: %-3d  Pending: %-3d\n",
				len(completed), len(running), len(failed), pending)
			output.F.Println()

			if len(completed) > 0 {
				output.F.Section("Completed")
				for _, w := range completed {
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
		},
	}

	return cmd
}

func repoName(workflowID string) string {
	return strings.TrimPrefix(workflowID, "investigate-single-repo-")
}

func repoNames(wfs []api.WorkflowExecution) []string {
	names := make([]string, len(wfs))
	for i, w := range wfs {
		names[i] = repoName(w.WorkflowID)
	}
	return names
}

func elapsed(startTime string) string {
	t, err := time.Parse(time.RFC3339Nano, startTime)
	if err != nil {
		// Try without nanoseconds
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
