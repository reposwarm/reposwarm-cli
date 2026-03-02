package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newWorkflowsRetryCmd() *cobra.Command {
	var (
		yes   bool
		model string
	)

	cmd := &cobra.Command{
		Use:   "retry <workflow-id>",
		Short: "Terminate a stuck workflow and re-investigate the same repo",
		Long: `Retry terminates the specified workflow and immediately starts a new
investigation for the same repository.

This is the recommended workflow when an investigation is stuck:
  1. Fix the root cause (env vars, worker config, etc.)
  2. reposwarm workflows retry <id>

Equivalent to:
  reposwarm workflows terminate <id> -y
  reposwarm investigate <repo>`,
		Args: friendlyExactArgs(1, "reposwarm workflows retry <workflow-id>\n\nExample:\n  reposwarm workflows retry investigate-is-odd-20260302"),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflowID := args[0]

			// Extract repo name from workflow ID
			repo := repoName(workflowID)
			if repo == "" || repo == workflowID {
				return fmt.Errorf("cannot extract repo name from workflow ID '%s'\n\nExpected format: investigate-<repo>-<date>\nYou can also manually run:\n  reposwarm workflows terminate %s -y\n  reposwarm investigate <repo-name>", workflowID, workflowID)
			}

			client, err := getClient()
			if err != nil {
				return err
			}

			if !yes && !flagJSON {
				fmt.Printf("  Retry investigation for '%s'?\n", repo)
				fmt.Printf("  This will terminate workflow %s and start a new investigation.\n", workflowID)
				fmt.Printf("  Continue? [y/N] ")
				var confirm string
				fmt.Scanln(&confirm)
				if strings.ToLower(confirm) != "y" {
					output.F.Info("Cancelled")
					return nil
				}
			}

			// 1. Terminate the stuck workflow
			if !flagJSON {
				output.F.Printf("  Terminating workflow %s...\n", workflowID)
			}

			body := map[string]string{"reason": "Retried via CLI"}
			var termResult any
			if err := client.Post(ctx(), "/workflows/"+workflowID+"/terminate", body, &termResult); err != nil {
				if !flagJSON {
					output.F.Warning(fmt.Sprintf("Terminate returned error (may already be done): %v", err))
				}
			} else if !flagJSON {
				output.F.Printf("  %s Terminated %s\n", output.Green("✓"), workflowID)
			}

			time.Sleep(500 * time.Millisecond)

			// 2. Start new investigation
			if !flagJSON {
				output.F.Printf("  Starting new investigation for '%s'...\n", repo)
			}

			req := api.InvestigateRequest{
				RepoName: repo,
			}
			if model != "" {
				req.Model = model
			}

			var investResult struct {
				WorkflowID string `json:"workflowId"`
				Message    string `json:"message"`
			}
			if err := client.Post(ctx(), "/investigate/single", req, &investResult); err != nil {
				return fmt.Errorf("failed to start investigation: %w\n\nWorkflow was terminated. Start manually:\n  reposwarm investigate %s", err, repo)
			}

			if flagJSON {
				return output.JSON(map[string]any{
					"terminated":    workflowID,
					"repo":          repo,
					"newWorkflowId": investResult.WorkflowID,
					"status":        "retried",
				})
			}

			output.F.Printf("  %s Started new investigation: %s\n", output.Green("✓"), investResult.WorkflowID)
			output.F.Println()
			output.F.Success(fmt.Sprintf("Retried '%s' — track with: reposwarm wf status %s -v", repo, investResult.WorkflowID))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation")
	cmd.Flags().StringVar(&model, "model", "", "Override model for retry")
	return cmd
}
