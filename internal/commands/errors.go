package commands

import (
	"fmt"
	"strings"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

// WorkflowError represents a single error from a workflow's history.
type WorkflowError struct {
	WorkflowID string `json:"workflowId"`
	Repo       string `json:"repo"`
	EventType  string `json:"eventType"`
	Summary    string `json:"summary"`
	Detail     string `json:"detail,omitempty"`
	Timestamp  string `json:"timestamp"`
	ActivityID string `json:"activityId,omitempty"`
}

func newErrorsCmd() *cobra.Command {
	var repo string
	var limit int

	cmd := &cobra.Command{
		Use:   "errors",
		Short: "Show errors from investigation workflows",
		Long: `Lists errors from recent investigation workflows.
Shows activity failures, workflow failures, and timeouts.

Examples:
  reposwarm errors                # All recent errors
  reposwarm errors --repo is-odd  # Errors for a specific repo`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			// Get workflows
			var result api.WorkflowsResponse
			if err := client.Get(ctx(), fmt.Sprintf("/workflows?pageSize=%d", limit), &result); err != nil {
				return err
			}

			var allErrors []WorkflowError
			for _, w := range result.Executions {
				if w.Type != "InvestigateSingleRepoWorkflow" {
					continue
				}
				name := repoName(w.WorkflowID)
				if repo != "" && name != repo {
					continue
				}

				errors := getWorkflowErrors(client, w.WorkflowID)
				allErrors = append(allErrors, errors...)
			}

			if flagJSON {
				return output.JSON(allErrors)
			}

			if len(allErrors) == 0 {
				if repo != "" {
					output.F.Success(fmt.Sprintf("No errors found for '%s' 🎉", repo))
				} else {
					output.F.Success("No errors found in recent investigations 🎉")
				}
				return nil
			}

			title := fmt.Sprintf("Errors (%d)", len(allErrors))
			if repo != "" {
				title = fmt.Sprintf("Errors for %s (%d)", repo, len(allErrors))
			}
			output.F.Section(title)

			// Group by repo
			byRepo := map[string][]WorkflowError{}
			var repoOrder []string
			for _, e := range allErrors {
				if _, seen := byRepo[e.Repo]; !seen {
					repoOrder = append(repoOrder, e.Repo)
				}
				byRepo[e.Repo] = append(byRepo[e.Repo], e)
			}

			for _, r := range repoOrder {
				errors := byRepo[r]
				fmt.Printf("  %s %s\n", output.Bold(r), output.Dim(fmt.Sprintf("(%d errors)", len(errors))))
				fmt.Println()
				for _, e := range errors {
					fmt.Printf("    %s  %s\n", output.Red("✗"), e.Summary)
					if e.Detail != "" {
						for _, line := range strings.Split(e.Detail, "\n") {
							if line = strings.TrimSpace(line); line != "" {
								fmt.Printf("       %s\n", output.Dim(truncate(line, 80)))
							}
						}
					}
					if e.ActivityID != "" {
						fmt.Printf("       %s %s\n", output.Dim("Activity:"), output.Dim(e.ActivityID))
					}
					fmt.Printf("       %s %s\n", output.Dim("Time:"), output.Dim(e.Timestamp))
					fmt.Println()
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "Filter errors by repo name")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max workflows to scan")
	return cmd
}

// getWorkflowErrors extracts errors from a workflow's event history.
func getWorkflowErrors(client *api.Client, workflowID string) []WorkflowError {
	repo := repoName(workflowID)

	var histResp struct {
		Data api.WorkflowHistory `json:"data"`
	}
	if err := client.Get(ctx(), fmt.Sprintf("/workflows/%s/history", workflowID), &histResp); err != nil {
		return nil
	}

	var errors []WorkflowError
	// Track activity names by scheduled event ID
	activityNames := map[string]string{}

	for _, event := range histResp.Data.Events {
		eventType, _ := event["eventType"].(string)
		eventTime, _ := event["eventTime"].(string)
		details, _ := event["details"].(map[string]any)
		eventID, _ := event["eventId"].(string)

		// Track activity names
		if eventType == "EVENT_TYPE_ACTIVITY_TASK_SCHEDULED" && details != nil {
			actType, _ := details["activityType"].(map[string]any)
			if actType != nil {
				name, _ := actType["name"].(string)
				activityNames[eventID] = name
			}
			// Also check nested
			if at, ok := details["activityTaskScheduledEventAttributes"].(map[string]any); ok {
				if actType, ok := at["activityType"].(map[string]any); ok {
					name, _ := actType["name"].(string)
					activityNames[eventID] = name
				}
			}
		}

		// Activity failures
		if eventType == "EVENT_TYPE_ACTIVITY_TASK_FAILED" && details != nil {
			summary, detail, activityID := extractActivityFailure(details, activityNames)
			if summary != "" {
				errors = append(errors, WorkflowError{
					WorkflowID: workflowID,
					Repo:       repo,
					EventType:  "activity_failed",
					Summary:    summary,
					Detail:     detail,
					Timestamp:  formatEventTime(eventTime),
					ActivityID: activityID,
				})
			}
		}

		// Activity timeouts
		if eventType == "EVENT_TYPE_ACTIVITY_TASK_TIMED_OUT" {
			scheduledID := ""
			if details != nil {
				if sid, ok := details["scheduledEventId"].(string); ok {
					scheduledID = sid
				}
			}
			actName := activityNames[scheduledID]
			if actName == "" {
				actName = "unknown activity"
			}
			errors = append(errors, WorkflowError{
				WorkflowID: workflowID,
				Repo:       repo,
				EventType:  "activity_timeout",
				Summary:    fmt.Sprintf("Activity timed out: %s", actName),
				Timestamp:  formatEventTime(eventTime),
				ActivityID: actName,
			})
		}

		// Workflow failures
		if eventType == "EVENT_TYPE_WORKFLOW_EXECUTION_FAILED" && details != nil {
			summary, detail := extractWorkflowFailure(details)
			errors = append(errors, WorkflowError{
				WorkflowID: workflowID,
				Repo:       repo,
				EventType:  "workflow_failed",
				Summary:    summary,
				Detail:     detail,
				Timestamp:  formatEventTime(eventTime),
			})
		}

		// Workflow timeouts
		if eventType == "EVENT_TYPE_WORKFLOW_EXECUTION_TIMED_OUT" {
			errors = append(errors, WorkflowError{
				WorkflowID: workflowID,
				Repo:       repo,
				EventType:  "workflow_timeout",
				Summary:    "Workflow timed out",
				Timestamp:  formatEventTime(eventTime),
			})
		}

		// Workflow terminated
		if eventType == "EVENT_TYPE_WORKFLOW_EXECUTION_TERMINATED" {
			reason := ""
			if details != nil {
				reason, _ = details["reason"].(string)
			}
			if reason == "" {
				reason = "Workflow was terminated"
			}
			errors = append(errors, WorkflowError{
				WorkflowID: workflowID,
				Repo:       repo,
				EventType:  "workflow_terminated",
				Summary:    reason,
				Timestamp:  formatEventTime(eventTime),
			})
		}
	}

	return errors
}

func extractActivityFailure(details map[string]any, activityNames map[string]string) (summary, detail, activityID string) {
	// Navigate the nested failure structure
	failure := findNested(details, "failure")
	if failure == nil {
		// Try direct
		if msg, ok := details["message"].(string); ok {
			return msg, "", ""
		}
		return "Activity failed (no details)", "", ""
	}

	msg, _ := failure["message"].(string)
	if msg == "" {
		msg = "Activity failed"
	}

	// Get activity name from scheduled event ID
	if sid, ok := details["scheduledEventId"].(string); ok {
		activityID = activityNames[sid]
	}
	if activityID != "" {
		summary = fmt.Sprintf("%s: %s", activityID, msg)
	} else {
		summary = msg
	}

	// Get stack trace or cause
	if st, ok := failure["stackTrace"].(string); ok && st != "" {
		detail = st
	}
	if cause, ok := failure["cause"].(map[string]any); ok {
		if causeMsg, ok := cause["message"].(string); ok {
			if detail != "" {
				detail += "\n"
			}
			detail += "Caused by: " + causeMsg
		}
	}

	return summary, detail, activityID
}

func extractWorkflowFailure(details map[string]any) (summary, detail string) {
	failure := findNested(details, "failure")
	if failure == nil {
		return "Workflow failed (no details)", ""
	}

	msg, _ := failure["message"].(string)
	if msg == "" {
		msg = "Workflow failed"
	}

	if st, ok := failure["stackTrace"].(string); ok && st != "" {
		detail = st
	}

	return msg, detail
}

// findNested looks for a key in a map, including one level of nesting.
func findNested(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	// Check nested attributes
	for _, v := range m {
		if sub, ok := v.(map[string]any); ok {
			if found, ok := sub[key].(map[string]any); ok {
				return found
			}
		}
	}
	return nil
}

func formatEventTime(t string) string {
	if len(t) > 19 {
		return t[:19]
	}
	return t
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
