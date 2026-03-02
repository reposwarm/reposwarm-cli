package commands

import (
	"fmt"
	"strings"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
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
	cmd.AddCommand(newWorkflowsTerminateCmd())
	cmd.AddCommand(newWorkflowsProgressCmd())
	cmd.AddCommand(newWatchCmd())
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
	return &cobra.Command{
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
			F.KeyValue("Started", wf.StartTime)
			if wf.CloseTime != "" {
				F.KeyValue("Closed", wf.CloseTime)
			}
			F.Println()
			return nil
		},
	}
}

func newWorkflowsTerminateCmd() *cobra.Command {
	var yes bool
	var reason string

	cmd := &cobra.Command{
		Use:   "terminate <workflow-id>",
		Short: "Terminate a running workflow",
		Args:  friendlyExactArgs(1, "reposwarm workflows terminate <workflow-id>\n\nExample:\n  reposwarm workflows terminate wf-12345"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				fmt.Printf("  Terminate workflow %s? [y/N] ", args[0])
				var confirm string
				fmt.Scanln(&confirm)
				if strings.ToLower(confirm) != "y" {
					output.F.Info("Cancelled")
					return nil
				}
			}

			client, err := getClient()
			if err != nil {
				return err
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
	cmd.Flags().StringVar(&reason, "reason", "Terminated via CLI", "Termination reason")
	return cmd
}
