package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newWorkflowsHistoryCmd() *cobra.Command {
	var (
		runID  string
		filter string
		limit  int
	)

	cmd := &cobra.Command{
		Use:   "history <workflow-id>",
		Short: "Show Temporal workflow event history",
		Long: `Show Temporal workflow event history for debugging stuck/failed workflows.

Displays workflow execution events with meaningful details:
- Activity scheduling, starts, completions, and failures
- Timer events
- Workflow start/completion/failure
- Worker identity and task durations

Use --filter to search for specific event types (case-insensitive).`,
		Args: friendlyExactArgs(1, "reposwarm workflows history <workflow-id>\n\nExample:\n  reposwarm workflows history investigate-my-app-20260302"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			// Build request path
			path := fmt.Sprintf("/workflows/%s/history", args[0])
			if runID != "" {
				path += fmt.Sprintf("?runId=%s", runID)
			}

			// Get history from API
			var response struct {
				Data struct {
					Events []map[string]any `json:"events"`
				} `json:"data"`
			}
			if err := client.Get(ctx(), path, &response); err != nil {
				return err
			}

			events := response.Data.Events

			// Apply filter if provided
			if filter != "" {
				var filtered []map[string]any
				filterLower := strings.ToLower(filter)
				for _, event := range events {
					eventType, _ := event["eventType"].(string)
					if strings.Contains(strings.ToLower(eventType), filterLower) {
						filtered = append(filtered, event)
					}
				}
				events = filtered
			}

			// Apply limit
			if limit > 0 && len(events) > limit {
				events = events[:limit]
			}

			// JSON output
			if flagJSON {
				return output.JSON(events)
			}

			// Human-readable output
			F := output.F
			title := fmt.Sprintf("Workflow History: %s (%d events)", args[0], len(events))
			if filter != "" {
				title = fmt.Sprintf("Workflow History: %s (%d events, filtered by '%s')", args[0], len(events), filter)
			}
			F.Section(title)
			F.Println()

			// Track activity schedules for duration calculation
			activityScheduled := make(map[string]time.Time)

			for _, event := range events {
				eventID, _ := event["eventId"].(float64)
				eventTime, _ := event["eventTime"].(string)
				eventType, _ := event["eventType"].(string)
				details, _ := event["details"].(map[string]any)

				// Parse event time
				var eventTimeStr string
				if t, err := time.Parse(time.RFC3339, eventTime); err == nil {
					eventTimeStr = t.Format("2006-01-02 15:04:05")
				} else {
					eventTimeStr = eventTime
				}

				// Format event header
				F.Printf("  #%-3.0f  %s  %s\n", eventID, eventTimeStr, output.Bold(eventType))

				// Show event-specific details
				switch eventType {
				case "WorkflowExecutionStarted":
					if input, ok := details["input"].(string); ok && input != "" {
						// Truncate long input
						if len(input) > 200 {
							input = input[:197] + "..."
						}
						F.Printf("      Input: %s\n", input)
					}

				case "ActivityTaskScheduled":
					if activityType, ok := details["activityType"].(string); ok {
						F.Printf("      Activity: %s\n", activityType)
						// Store schedule time for duration calculation
						if t, err := time.Parse(time.RFC3339, eventTime); err == nil {
							activityScheduled[activityType] = t
						}
					}
					if taskQueue, ok := details["taskQueue"].(string); ok {
						F.Printf("      TaskQueue: %s\n", taskQueue)
					}

				case "ActivityTaskStarted":
					if identity, ok := details["identity"].(string); ok {
						F.Printf("      Identity: %s\n", identity)
					}

				case "ActivityTaskCompleted":
					// Calculate duration if we have the scheduled time
					if activityType, ok := details["activityType"].(string); ok {
						if schedTime, exists := activityScheduled[activityType]; exists {
							if t, err := time.Parse(time.RFC3339, eventTime); err == nil {
								duration := t.Sub(schedTime)
								F.Printf("      Duration: %s\n", formatDuration(duration))
							}
						}
					}

				case "ActivityTaskFailed":
					if failure, ok := details["failure"].(map[string]any); ok {
						if message, ok := failure["message"].(string); ok {
							F.Printf("      %s\n", output.Red("Failure: "+message))
						}
						if cause, ok := failure["cause"].(string); ok {
							F.Printf("      %s\n", output.Red("Cause: "+cause))
						}
					}

				case "ActivityTaskTimedOut":
					if timeoutType, ok := details["timeoutType"].(string); ok {
						F.Printf("      %s\n", output.Yellow("Timeout Type: "+timeoutType))
					}

				case "WorkflowExecutionCompleted":
					if result, ok := details["result"].(string); ok && result != "" {
						// Truncate long result
						if len(result) > 200 {
							result = result[:197] + "..."
						}
						F.Printf("      Result: %s\n", result)
					}

				case "WorkflowExecutionFailed":
					if failure, ok := details["failure"].(map[string]any); ok {
						if message, ok := failure["message"].(string); ok {
							F.Printf("      %s\n", output.Red("Failure: "+message))
						}
					}

				case "WorkflowExecutionTerminated":
					if reason, ok := details["reason"].(string); ok {
						F.Printf("      Reason: %s\n", reason)
					}

				case "TimerStarted":
					if delay, ok := details["startToFireTimeout"].(string); ok {
						F.Printf("      Delay: %s\n", delay)
					}

				case "TimerFired":
					if timerID, ok := details["timerId"].(string); ok {
						F.Printf("      Timer ID: %s\n", timerID)
					}
				}

				F.Println()
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&runID, "run-id", "", "Optional Temporal run ID")
	cmd.Flags().StringVar(&filter, "filter", "", "Filter events by type (case-insensitive substring match)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max events to show (0 = unlimited)")

	return cmd
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
}