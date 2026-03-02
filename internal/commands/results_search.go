package commands

import (
	"fmt"
	"strings"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newResultsSearchCmd() *cobra.Command {
	var repoFilter string
	var sectionFilter string
	var maxHits int

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search across investigation results",
		Long: `Search for text across investigation results.

Without filters, searches all repos (can be slow for many repos).
Use --repo to limit to a specific repo, --section for a specific section.

Examples:
  reposwarm results search "Cognito" --repo my-app
  reposwarm results search "DynamoDB" --section DBs
  reposwarm results search "security" --max 20`,
		Args: friendlyExactArgs(1, "reposwarm results search <query>\n\nExample:\n  reposwarm results search \"DynamoDB\" --repo my-app"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			query := strings.ToLower(args[0])

			type SearchHit struct {
				Repo    string `json:"repo"`
				Section string `json:"section"`
				Line    string `json:"line"`
			}

			var hits []SearchHit
			done := false

			// Get repo list
			var repos []string
			if repoFilter != "" {
				repos = []string{repoFilter}
			} else {
				var repoList api.WikiReposResponse
				if err := client.Get(ctx(), "/wiki", &repoList); err != nil {
					return err
				}
				for _, r := range repoList.Repos {
					repos = append(repos, r.Name)
				}
			}

			for _, repoName := range repos {
				if done {
					break
				}
				var index api.WikiIndex
				if err := client.Get(ctx(), "/wiki/"+repoName, &index); err != nil {
					continue
				}
				for _, s := range index.Sections {
					if done {
						break
					}
					sName := s.Name()
					if sectionFilter != "" && sName != sectionFilter {
						continue
					}
					var content api.WikiContent
					if err := client.Get(ctx(), "/wiki/"+repoName+"/"+sName, &content); err != nil {
						continue
					}
					for _, line := range strings.Split(content.Content, "\n") {
						if strings.Contains(strings.ToLower(line), query) {
							trimmed := strings.TrimSpace(line)
							if trimmed == "" {
								continue
							}
							// Truncate long lines
							if len(trimmed) > 200 {
								trimmed = trimmed[:200] + "..."
							}
							hits = append(hits, SearchHit{
								Repo:    repoName,
								Section: sName,
								Line:    trimmed,
							})
							if maxHits > 0 && len(hits) >= maxHits {
								done = true
								break
							}
						}
					}
				}
			}

			if flagJSON {
				return output.JSON(hits)
			}

			F := output.F
			suffix := ""
			if done && maxHits > 0 {
				suffix = fmt.Sprintf(", limited to %d", maxHits)
			}
			F.Section(fmt.Sprintf("Search '%s' (%d hits%s)", args[0], len(hits), suffix))

			if len(hits) == 0 {
				F.Info("No results found")
				return nil
			}

			// Group by repo/section
			lastCtx := ""
			for _, h := range hits {
				ctx := h.Repo + "/" + h.Section
				if ctx != lastCtx {
					F.Printf("\n%s\n", ctx)
					lastCtx = ctx
				}
				F.Printf("  %s\n", h.Line)
			}
			F.Println()
			return nil
		},
	}

	cmd.Flags().StringVar(&repoFilter, "repo", "", "Limit search to specific repo")
	cmd.Flags().StringVar(&sectionFilter, "section", "", "Limit search to specific section")
	cmd.Flags().IntVar(&maxHits, "max", 50, "Maximum number of hits (0 = unlimited)")
	return cmd
}
