package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/reposwarm/reposwarm-cli/internal/api"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newWorkflowsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workflows",
		Aliases: []string{"wf"},
		Short:   "Manage Temporal workflows",
	}
	cmd.AddCommand(newWorkflowsListCmd())
	cmd.AddCommand(newWorkflowsStatusCmd())
	cmd.AddCommand(newWorkflowsHistoryCmd())
	cmd.AddCommand(newWorkflowsTerminateCmd())
	cmd.AddCommand(newWorkflowsWatchRepoCmd())
	cmd.AddCommand(newWatchCmd())
	cmd.AddCommand(newWorkflowsRetryCmd())
	cmd.AddCommand(newWorkflowsPruneCmd())
	cmd.AddCommand(newWorkflowsCancelCmd())
	return cmd
}

func newWorkflowsListCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent workflows",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			var result api.WorkflowsResponse
			path := fmt.Sprintf("/workflows?pageSize=%d", limit)
			if err := client.Get(ctx(), path, &result); err != nil {
				return err
			}

			if flagJSON {
				return output.JSON(result.Executions)
			}

			F := output.F
			F.Section(fmt.Sprintf("Workflows (%d workflows)", len(result.Executions)))
			headers := []string{"Workflow ID", "Status", "Type", "Started"}
			var rows [][]string
			for _, w := range result.Executions {
				wfID := w.WorkflowID
				if len(wfID) > 50 {
					wfID = wfID[:47] + "..."
				}
				rows = append(rows, []string{
					wfID,
					F.StatusText(w.Status),
					w.Type,
					w.StartTime,
				})
			}
			F.Table(headers, rows)
			F.Println()
			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 25, "Max workflows to show")
	return cmd
}

func newWorkflowsStatusCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "status <workflow-id>",
		Short: "Show detailed workflow status",
		Args:  friendlyExactArgs(1, "reposwarm workflows status <workflow-id>\n\nExample:\n  reposwarm workflows status wf-12345"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			var wf api.WorkflowExecution
			if err := client.Get(ctx(), "/workflows/"+args[0], &wf); err != nil {
				return err
			}

			if flagJSON {
				return output.JSON(wf)
			}

			F := output.F
			F.Section("Workflow Details")
			F.KeyValue("ID", wf.WorkflowID)
			F.KeyValue("Run ID", wf.RunID)
			F.KeyValue("Status", F.StatusText(wf.Status))
			F.KeyValue("Type", wf.Type)

			// Parse start time for relative duration
			var startTime time.Time
			if t, err := time.Parse(time.RFC3339, wf.StartTime); err == nil {
				startTime = t
				duration := time.Since(t)
				F.KeyValue("Started", fmt.Sprintf("%s (%s ago)", wf.StartTime, formatRelativeTime(duration)))
			} else {
				F.KeyValue("Started", wf.StartTime)
			}

			if wf.CloseTime != "" {
				F.KeyValue("Closed", wf.CloseTime)
			}

			// If verbose, fetch and show activity details
			if verbose {
				F.Println()
				if err := showActivityDetails(client, args[0], startTime); err != nil {
					F.Warning(fmt.Sprintf("Could not fetch activity details: %v", err))
				}

				// Worker attribution — key diagnostic info
				F.Println()
				F.Section("Worker Status")
				workers := gatherWorkerInfo(client)
				healthy := 0
				for _, w := range workers {
					if w.Status == "healthy" {
						healthy++
					}
				}
				queue := ""
				if wf.TaskQueue != "" {
					queue = wf.TaskQueue
				} else {
					queue = "investigate-task-queue"
				}
				F.KeyValue("Task Queue", queue)
				F.KeyValue("Healthy Workers", fmt.Sprintf("%d of %d", healthy, len(workers)))
				for _, w := range workers {
					icon := output.Green("✅")
					if w.Status != "healthy" {
						icon = output.Red("❌")
					}
					detail := w.Status
					if len(w.EnvErrors) > 0 {
						detail += fmt.Sprintf(" (%d env errors)", len(w.EnvErrors))
					}
					F.Printf("  %s %s: %s\n", icon, w.Name, detail)
				}

				// Diagnosis
				if healthy == 0 && len(workers) > 0 {
					F.Println()
					F.Section("Diagnosis")
					F.Warning("No healthy workers available on '" + queue + "'")
					F.Info("Run: reposwarm workers list to see worker status")
					F.Info("Run: reposwarm doctor for full diagnostics")
				}
			}

			F.Println()
			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show activity details from workflow history")
	return cmd
}

func newWorkflowsTerminateCmd() *cobra.Command {
	var yes bool
	var all bool
	var reason string

	cmd := &cobra.Command{
		Use:   "terminate [workflow-id]",
		Short: "Terminate a running workflow",
		Long: `Terminate one or all running workflows.

Examples:
  reposwarm workflows terminate wf-12345
  reposwarm workflows terminate --all --yes`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) == 0 {
				return fmt.Errorf("specify a workflow ID or use --all\n\nExamples:\n  reposwarm workflows terminate <workflow-id>\n  reposwarm workflows terminate --all --yes")
			}

			client, err := getClient()
			if err != nil {
				return err
			}

			if all {
				// Fetch running workflows
				var wfResult api.WorkflowsResponse
				if err := client.Get(ctx(), "/workflows?pageSize=100", &wfResult); err != nil {
					return fmt.Errorf("fetching workflows: %w", err)
				}

				var running []api.WorkflowExecution
				for _, w := range wfResult.Executions {
					if strings.EqualFold(w.Status, "Running") {
						running = append(running, w)
					}
				}

				if len(running) == 0 {
					if flagJSON {
						return output.JSON(map[string]any{"terminated": 0})
					}
					output.F.Info("No running workflows to terminate")
					return nil
				}

				if !yes && !flagJSON {
					fmt.Printf("  Terminate %d running workflow(s)? [y/N] ", len(running))
					var confirm string
					fmt.Scanln(&confirm)
					if strings.ToLower(confirm) != "y" {
						output.F.Info("Cancelled")
						return nil
					}
				}

				terminated := 0
				failed := 0
				for _, w := range running {
					body := map[string]string{"reason": reason}
					var result any
					if err := client.Post(ctx(), "/workflows/"+w.WorkflowID+"/terminate", body, &result); err != nil {
						failed++
						if !flagJSON {
							output.F.Warning(fmt.Sprintf("Failed to terminate %s: %v", w.WorkflowID, err))
						}
						continue
					}
					terminated++
					if !flagJSON {
						output.F.Success(fmt.Sprintf("Terminated %s", w.WorkflowID))
					}
				}

				if flagJSON {
					return output.JSON(map[string]any{
						"terminated": terminated,
						"failed":    failed,
						"total":     len(running),
					})
				}
				output.F.Println()
				output.Successf("Terminated %d/%d running workflows", terminated, len(running))
				return nil
			}

			// Single workflow terminate
			if !yes && !flagJSON {
				fmt.Printf("  Terminate workflow %s? [y/N] ", args[0])
				var confirm string
				fmt.Scanln(&confirm)
				if strings.ToLower(confirm) != "y" {
					output.F.Info("Cancelled")
					return nil
				}
			}

			body := map[string]string{"reason": reason}
			var result any
			if err := client.Post(ctx(), "/workflows/"+args[0]+"/terminate", body, &result); err != nil {
				return err
			}

			if flagJSON {
				return output.JSON(map[string]any{"workflowId": args[0], "terminated": true})
			}
			output.F.Success(fmt.Sprintf("Terminated workflow %s", args[0]))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation")
	cmd.Flags().BoolVar(&all, "all", false, "Terminate all running workflows")
	cmd.Flags().StringVar(&reason, "reason", "Terminated via CLI", "Termination reason")
	return cmd
}

// showActivityDetails fetches workflow history and displays activity status
func showActivityDetails(client *api.Client, workflowID string, startTime time.Time) error {
	// Fetch workflow history
	var response struct {
		Data struct {
			Events []map[string]any `json:"events"`
		} `json:"data"`
	}
	if err := client.Get(ctx(), fmt.Sprintf("/workflows/%s/history", workflowID), &response); err != nil {
		return err
	}

	// Track activities
	type activityInfo struct {
		name      string
		status    string // pending, running, completed, failed
		startTime time.Time
		endTime   time.Time
		worker    string
		error     string
	}

	activities := make(map[string]*activityInfo)
	var activityOrder []string

	// Process events to build activity status
	for _, event := range response.Data.Events {
		eventType, _ := event["eventType"].(string)
		details, _ := event["details"].(map[string]any)
		eventTime, _ := event["eventTime"].(string)

		var eventTs time.Time
		if t, err := time.Parse(time.RFC3339, eventTime); err == nil {
			eventTs = t
		}

		switch eventType {
		case "ActivityTaskScheduled":
			if activityType, ok := details["activityType"].(string); ok {
				if _, exists := activities[activityType]; !exists {
					activities[activityType] = &activityInfo{
						name:      activityType,
						status:    "pending",
						startTime: eventTs,
					}
					activityOrder = append(activityOrder, activityType)
				}
			}

		case "ActivityTaskStarted":
			if activityType, ok := details["activityType"].(string); ok {
				if act, exists := activities[activityType]; exists {
					act.status = "running"
					if identity, ok := details["identity"].(string); ok {
						act.worker = identity
					}
				}
			}

		case "ActivityTaskCompleted":
			if activityType, ok := details["activityType"].(string); ok {
				if act, exists := activities[activityType]; exists {
					act.status = "completed"
					act.endTime = eventTs
				}
			}

		case "ActivityTaskFailed":
			if activityType, ok := details["activityType"].(string); ok {
				if act, exists := activities[activityType]; exists {
					act.status = "failed"
					act.endTime = eventTs
					if failure, ok := details["failure"].(map[string]any); ok {
						if msg, ok := failure["message"].(string); ok {
							act.error = msg
						}
					}
				}
			}

		case "ActivityTaskTimedOut":
			if activityType, ok := details["activityType"].(string); ok {
				if act, exists := activities[activityType]; exists {
					act.status = "timeout"
					act.endTime = eventTs
				}
			}
		}
	}

	// Display activity status
	F := output.F
	F.Section("Activities")
	for _, name := range activityOrder {
		act := activities[name]

		statusIcon := "○" // pending
		statusText := "pending"
		switch act.status {
		case "running":
			statusIcon = "⠹"
			statusText = output.Yellow("running")
		case "completed":
			statusIcon = "✓"
			statusText = output.Green("completed")
		case "failed":
			statusIcon = "✗"
			statusText = output.Red("failed")
		case "timeout":
			statusIcon = "⏱"
			statusText = output.Yellow("timeout")
		}

		// Calculate duration
		durationStr := ""
		if act.status == "completed" || act.status == "failed" || act.status == "timeout" {
			if !act.endTime.IsZero() && !act.startTime.IsZero() {
				duration := act.endTime.Sub(act.startTime)
				durationStr = fmt.Sprintf("%10s", formatDuration(duration))
			}
		} else if act.status == "running" {
			if !act.startTime.IsZero() {
				duration := time.Since(act.startTime)
				durationStr = fmt.Sprintf("%10s", formatDuration(duration))
			}
		}

		// Format activity name (truncate if too long)
		actName := act.name
		if len(actName) > 35 {
			actName = actName[:32] + "..."
		}

		line := fmt.Sprintf("  %s %-35s %s   %s", statusIcon, actName, durationStr, statusText)
		if act.status == "running" && act.worker != "" {
			line += fmt.Sprintf(" (worker: %s)", act.worker)
		}
		if act.error != "" {
			line += fmt.Sprintf(" - %s", act.error)
		}
		F.Println(line)
	}

	// Show last event and total events
	F.Println()
	if len(response.Data.Events) > 0 {
		lastEvent := response.Data.Events[len(response.Data.Events)-1]
		eventID, _ := lastEvent["eventId"].(float64)
		eventType, _ := lastEvent["eventType"].(string)
		eventTime, _ := lastEvent["eventTime"].(string)

		if t, err := time.Parse(time.RFC3339, eventTime); err == nil {
			duration := time.Since(t)
			F.KeyValue("Last Event", fmt.Sprintf("#%.0f %s (%s ago)", eventID, eventType, formatRelativeTime(duration)))
		}
		F.KeyValue("Total Events", fmt.Sprintf("%d", len(response.Data.Events)))
	}

	return nil
}

// formatRelativeTime formats a duration as relative time (e.g., "5h2m ago")
func formatRelativeTime(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if mins > 0 {
		return fmt.Sprintf("%dh%dm", hours, mins)
	}
	return fmt.Sprintf("%dh", hours)
}
