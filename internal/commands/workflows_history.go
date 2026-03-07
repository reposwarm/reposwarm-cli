package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/reposwarm/reposwarm-cli/internal/output"
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
				Events []map[string]any `json:"events"`
			}
			if err := client.Get(ctx(), path, &response); err != nil {
				return err
			}

			events := response.Events

			// Apply filter if provided
			if filter != "" {
				var filtered []map[string]any
				filterLower := strings.ToLower(filter)
				for _, event := range events {
					eventType, _ := event["eventType"].(string)
					// Match against both raw and normalized event type
					normalized := strings.ReplaceAll(strings.TrimPrefix(eventType, "EVENT_TYPE_"), "_", "")
					if strings.Contains(strings.ToLower(eventType), filterLower) ||
						strings.Contains(strings.ToLower(normalized), filterLower) {
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
				eventID, _ := event["eventId"].(string)
				if eventID == "" {
					// Fallback for float64 encoding
					if eid, ok := event["eventId"].(float64); ok {
						eventID = fmt.Sprintf("%.0f", eid)
					}
				}
				eventTime, _ := event["eventTime"].(string)
				rawEventType, _ := event["eventType"].(string)

				// Normalize event type: strip EVENT_TYPE_ prefix, convert to readable form
				eventType := rawEventType
				eventType = strings.TrimPrefix(eventType, "EVENT_TYPE_")
				// Convert SNAKE_CASE to PascalCase for readability
				parts := strings.Split(eventType, "_")
				for i, p := range parts {
					if len(p) > 0 {
						parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
					}
				}
				eventType = strings.Join(parts, "")

				// Details may be nested or at top level
				details, _ := event["details"].(map[string]any)
				if details == nil {
					details = event // fallback: use event map as details
				}

				// Parse event time
				var eventTimeStr string
				if t, err := time.Parse(time.RFC3339, eventTime); err == nil {
					eventTimeStr = t.Format("2006-01-02 15:04:05")
				} else {
					eventTimeStr = eventTime
				}

				// Format event header
				F.Printf("  #%-3s  %s  %s\n", eventID, eventTimeStr, output.Bold(eventType))

				// Show event-specific details (match both raw and normalized types)
				switch {
				case strings.Contains(rawEventType, "WORKFLOW_EXECUTION_STARTED") || eventType == "WorkflowExecutionStarted":
					if input, ok := details["input"].(string); ok && input != "" {
						// Truncate long input
						if len(input) > 200 {
							input = input[:197] + "..."
						}
						F.Printf("      Input: %s\n", input)
					}

				case strings.Contains(rawEventType, "ACTIVITY_TASK_SCHEDULED") || eventType == "ActivityTaskScheduled":
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

				case strings.Contains(rawEventType, "ACTIVITY_TASK_STARTED") || eventType == "ActivityTaskStarted":
					if identity, ok := details["identity"].(string); ok {
						F.Printf("      Identity: %s\n", identity)
					}

				case strings.Contains(rawEventType, "ACTIVITY_TASK_COMPLETED") || eventType == "ActivityTaskCompleted":
					// Calculate duration if we have the scheduled time
					if activityType, ok := details["activityType"].(string); ok {
						if schedTime, exists := activityScheduled[activityType]; exists {
							if t, err := time.Parse(time.RFC3339, eventTime); err == nil {
								duration := t.Sub(schedTime)
								F.Printf("      Duration: %s\n", formatDuration(duration))
							}
						}
					}

				case strings.Contains(rawEventType, "ACTIVITY_TASK_FAILED") || eventType == "ActivityTaskFailed":
					if failure, ok := details["failure"].(map[string]any); ok {
						if message, ok := failure["message"].(string); ok {
							F.Printf("      %s\n", output.Red("Failure: "+message))
						}
						if cause, ok := failure["cause"].(string); ok {
							F.Printf("      %s\n", output.Red("Cause: "+cause))
						}
					}

				case strings.Contains(rawEventType, "ACTIVITY_TASK_TIMED_OUT") || eventType == "ActivityTaskTimedOut":
					if timeoutType, ok := details["timeoutType"].(string); ok {
						F.Printf("      %s\n", output.Yellow("Timeout Type: "+timeoutType))
					}

				case strings.Contains(rawEventType, "WORKFLOW_EXECUTION_COMPLETED") || eventType == "WorkflowExecutionCompleted":
					if result, ok := details["result"].(string); ok && result != "" {
						// Truncate long result
						if len(result) > 200 {
							result = result[:197] + "..."
						}
						F.Printf("      Result: %s\n", result)
					}

				case strings.Contains(rawEventType, "WORKFLOW_EXECUTION_FAILED") || eventType == "WorkflowExecutionFailed":
					if failure, ok := details["failure"].(map[string]any); ok {
						if message, ok := failure["message"].(string); ok {
							F.Printf("      %s\n", output.Red("Failure: "+message))
						}
					}

				case strings.Contains(rawEventType, "WORKFLOW_EXECUTION_TERMINATED") || eventType == "WorkflowExecutionTerminated":
					if reason, ok := details["reason"].(string); ok {
						F.Printf("      Reason: %s\n", reason)
					}

				case strings.Contains(rawEventType, "TIMER_STARTED") || eventType == "TimerStarted":
					if delay, ok := details["startToFireTimeout"].(string); ok {
						F.Printf("      Delay: %s\n", delay)
					}

				case strings.Contains(rawEventType, "TIMER_FIRED") || eventType == "TimerFired":
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