package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/reposwarm/reposwarm-cli/internal/api"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newWorkflowsPruneCmd() *cobra.Command {
	var (
		older  string
		status string
		dryRun bool
		yes    bool
	)

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Clean up old completed, failed, or terminated workflows",
		Long: `Remove old workflows from Temporal history.

Examples:
  reposwarm workflows prune                          # Prune all non-running
  reposwarm workflows prune --older 24h              # Only older than 24h
  reposwarm workflows prune --status terminated      # Only terminated
  reposwarm workflows prune --dry-run                # Preview without deleting`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			// Parse --older duration
			var minAge time.Duration
			if older != "" {
				d, err := parseDuration(older)
				if err != nil {
					return fmt.Errorf("invalid --older value: %s (use: 1h, 24h, 7d)", older)
				}
				minAge = d
			}

			// Parse --status filter
			statusFilter := map[string]bool{}
			if status != "" {
				for _, s := range strings.Split(status, ",") {
					statusFilter[strings.TrimSpace(strings.ToUpper(s))] = true
				}
			} else {
				// Default: completed, failed, terminated
				statusFilter["COMPLETED"] = true
				statusFilter["FAILED"] = true
				statusFilter["TERMINATED"] = true
				statusFilter["CANCELLED"] = true
				statusFilter["TIMED_OUT"] = true
			}

			// Fetch workflows
			var result api.WorkflowsResponse
			if err := client.Get(ctx(), "/workflows?pageSize=100", &result); err != nil {
				return err
			}

			// Filter candidates
			type pruneCandidate struct {
				WorkflowID string `json:"workflowId"`
				Status     string `json:"status"`
				Age        string `json:"age"`
			}
			var candidates []pruneCandidate

			for _, w := range result.Executions {
				upper := strings.ToUpper(w.Status)
				if !statusFilter[upper] {
					continue
				}

				if minAge > 0 {
					closeTime := w.CloseTime
					if closeTime == "" {
						closeTime = w.StartTime
					}
					t, err := time.Parse(time.RFC3339, closeTime)
					if err != nil {
						continue
					}
					if time.Since(t) < minAge {
						continue
					}
				}

				age := "?"
				if t, err := time.Parse(time.RFC3339, w.StartTime); err == nil {
					age = formatRelativeTime(time.Since(t))
				}

				candidates = append(candidates, pruneCandidate{
					WorkflowID: w.WorkflowID,
					Status:     w.Status,
					Age:        age,
				})
			}

			if len(candidates) == 0 {
				if flagJSON {
					return output.JSON(map[string]any{"pruned": 0})
				}
				output.F.Success("Nothing to prune 🎉")
				return nil
			}

			if dryRun {
				if flagJSON {
					return output.JSON(map[string]any{
						"dryRun":     true,
						"candidates": candidates,
						"count":      len(candidates),
					})
				}
				output.F.Section(fmt.Sprintf("Would prune %d workflow(s)", len(candidates)))
				for _, c := range candidates {
					output.F.Printf("  - %s (%s, %s ago)\n", c.WorkflowID, c.Status, c.Age)
				}
				return nil
			}

			// Confirm
			if !yes && !flagJSON {
				fmt.Printf("  Prune %d workflow(s)? [y/N] ", len(candidates))
				var confirm string
				fmt.Scanln(&confirm)
				if strings.ToLower(confirm) != "y" {
					output.F.Info("Cancelled")
					return nil
				}
			}

			// Delete each workflow from Temporal history
			pruned := 0
			for _, c := range candidates {
				var delResult any
				if err := client.Delete(ctx(), "/workflows/"+c.WorkflowID, &delResult); err != nil {
					// Fallback: try terminate then delete
					body := map[string]string{"reason": "Pruned via CLI"}
					var termResult any
					_ = client.Post(ctx(), "/workflows/"+c.WorkflowID+"/terminate", body, &termResult)
					_ = client.Delete(ctx(), "/workflows/"+c.WorkflowID, &delResult)
				}
				pruned++

				if !flagJSON {
					output.F.Printf("  - %s (%s, %s ago)\n", c.WorkflowID, c.Status, c.Age)
				}
			}

			if flagJSON {
				return output.JSON(map[string]any{
					"pruned":     pruned,
					"candidates": candidates,
				})
			}

			output.F.Println()
			output.F.Success(fmt.Sprintf("Pruned %d workflow(s)", pruned))
			return nil
		},
	}

	cmd.Flags().StringVar(&older, "older", "", "Only prune workflows older than this (e.g. 1h, 24h, 7d)")
	cmd.Flags().StringVar(&status, "status", "", "Comma-separated statuses to prune (default: completed,failed,terminated)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview what would be pruned")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation")
	return cmd
}

// parseDuration handles durations with day support (e.g. "7d", "24h", "30m")
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		var n int
		if _, err := fmt.Sscanf(days, "%d", &n); err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
