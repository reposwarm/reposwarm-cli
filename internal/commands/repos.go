package commands

import (
	"fmt"
	"strings"

	"github.com/reposwarm/reposwarm-cli/internal/api"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newReposCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repos",
		Short: "Manage tracked repositories",
	}
	cmd.AddCommand(newReposListCmd())
	cmd.AddCommand(newReposShowCmd())
	cmd.AddCommand(newReposAddCmd())
	cmd.AddCommand(newReposRemoveCmd())
	cmd.AddCommand(newReposEnableCmd())
	cmd.AddCommand(newReposDisableCmd())
	cmd.AddCommand(newDiscoverCmd())
	return cmd
}

func newReposListCmd() *cobra.Command {
	var source, filter string
	var enabled, disabled bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all tracked repositories",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			var repos []api.Repository
			if err := client.Get(ctx(), "/repos", &repos); err != nil {
				return err
			}

			var filtered []api.Repository
			for _, r := range repos {
				if source != "" && !strings.EqualFold(r.Source, source) {
					continue
				}
				if filter != "" && !strings.Contains(strings.ToLower(r.Name), strings.ToLower(filter)) {
					continue
				}
				if enabled && !r.Enabled {
					continue
				}
				if disabled && r.Enabled {
					continue
				}
				filtered = append(filtered, r)
			}

			if flagJSON {
				return output.JSON(filtered)
			}

			F := output.F
			F.Section(fmt.Sprintf("Repositories (%d repos)", len(filtered)))
			headers := []string{"Name", "Source", "Enabled", "Docs", "Status"}
			var rows [][]string
			for _, r := range filtered {
				en := "yes"
				if !r.Enabled {
					en = "no"
				}
				docs := ""
				if r.HasDocs {
					docs = "yes"
				}
				rows = append(rows, []string{r.Name, r.Source, en, docs, r.Status})
			}
			F.Table(headers, rows)
			F.Println()
			return nil
		},
	}

	cmd.Flags().StringVar(&source, "source", "", "Filter by source (CodeCommit, GitHub)")
	cmd.Flags().StringVar(&filter, "filter", "", "Filter by name (case-insensitive)")
	cmd.Flags().BoolVar(&enabled, "enabled", false, "Show only enabled repos")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "Show only disabled repos")
	return cmd
}

func newReposAddCmd() *cobra.Command {
	var urlFlag, source string

	cmd := &cobra.Command{
		Use:   "add <name-or-url>",
		Short: "Add a repository to track",
		Args:  friendlyExactArgs(1, "reposwarm repos add <url>\nreposwarm repos add <name> --url <url>\n\nExamples:\n  reposwarm repos add https://github.com/org/repo\n  reposwarm repos add my-repo --url https://github.com/org/repo"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			var name, url, finalSource string
			arg := args[0]

			// Check if args[0] is a URL
			isURL := strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")

			if isURL {
				// args[0] is a URL
				if urlFlag != "" {
					// --url overrides args[0] for the URL
					url = urlFlag
				} else {
					url = arg
				}

				// Extract name from the URL (use the final URL)
				parts := strings.Split(strings.TrimSuffix(url, "/"), "/")
				if len(parts) > 0 {
					name = parts[len(parts)-1]
					// Remove .git suffix if present
					name = strings.TrimSuffix(name, ".git")
				}
			} else {
				// args[0] is a name
				name = arg
				if urlFlag != "" {
					url = urlFlag
				} else {
					return fmt.Errorf("url is required: use 'reposwarm repos add <url>' or 'reposwarm repos add <name> --url <url>'")
				}
			}

			// Determine final source
			if cmd.Flags().Changed("source") {
				finalSource = source
			} else {
				// Auto-detect from URL
				if strings.Contains(url, "github.com") {
					finalSource = "GitHub"
				} else {
					finalSource = "CodeCommit" // default
				}
			}

			body := map[string]any{
				"name":   name,
				"url":    url,
				"source": finalSource,
			}

			var result any
			if err := client.Post(ctx(), "/repos", body, &result); err != nil {
				return err
			}

			if flagJSON {
				return output.JSON(result)
			}
			output.F.Success(fmt.Sprintf("Added repository %s", name))
			return nil
		},
	}

	cmd.Flags().StringVar(&urlFlag, "url", "", "Repository URL (optional if URL provided as argument)")
	cmd.Flags().StringVar(&source, "source", "CodeCommit", "Source (CodeCommit, GitHub) - auto-detected for GitHub URLs")
	return cmd
}

func newReposRemoveCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a tracked repository",
		Args:  friendlyExactArgs(1, "reposwarm repos remove <name>\n\nExample:\n  reposwarm repos remove my-repo"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				fmt.Printf("  Remove %s? [y/N] ", args[0])
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

			var result any
			if err := client.Delete(ctx(), "/repos/"+args[0], &result); err != nil {
				return err
			}

			if flagJSON {
				return output.JSON(result)
			}
			output.F.Success(fmt.Sprintf("Removed repository %s", args[0]))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation")
	return cmd
}

func newReposEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <name>",
		Short: "Enable a repository for investigation",
		Args:  friendlyExactArgs(1, "reposwarm repos enable <name>\n\nExample:\n  reposwarm repos enable my-repo"),
		RunE:  repoToggle(true),
	}
}

func newReposDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <name>",
		Short: "Disable a repository from investigation",
		Args:  friendlyExactArgs(1, "reposwarm repos disable <name>\n\nExample:\n  reposwarm repos disable my-repo"),
		RunE:  repoToggle(false),
	}
}

func repoToggle(enable bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		client, err := getClient()
		if err != nil {
			return err
		}

		body := map[string]any{"enabled": enable}
		var result any
		if err := client.Patch(ctx(), "/repos/"+args[0], body, &result); err != nil {
			return err
		}

		action := "Enabled"
		if !enable {
			action = "Disabled"
		}
		if flagJSON {
			return output.JSON(map[string]any{"name": args[0], "enabled": enable})
		}
		output.F.Success(fmt.Sprintf("%s repository %s", action, args[0]))
		return nil
	}
}

func newReposShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show detailed info for a single repository",
		Args:  friendlyExactArgs(1, "reposwarm repos show <name>\n\nExample:\n  reposwarm repos show my-repo"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			var repo api.Repository
			if err := client.Get(ctx(), "/repos/"+args[0], &repo); err != nil {
				return err
			}

			if flagJSON {
				return output.JSON(repo)
			}

			F := output.F
			F.Section("Repository: " + repo.Name)
			F.KeyValue("Source", repo.Source)
			F.KeyValue("URL", repo.URL)
			F.KeyValue("Enabled", fmt.Sprint(repo.Enabled))
			F.KeyValue("Has Docs", fmt.Sprint(repo.HasDocs))
			F.KeyValue("Status", repo.Status)
			if repo.Description != "" {
				F.KeyValue("Description", repo.Description)
			}
			F.Println()
			return nil
		},
	}
}
