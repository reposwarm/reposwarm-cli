package commands

import (
	"fmt"
	"strings"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <repo1> <repo2> [section]",
		Short: "Compare investigation results between two repos",
		Long: `Compare investigation results side-by-side.

Shows sections present in one but not the other, and line count differences.

Examples:
  reposwarm results diff is-odd meshmart-catalog
  reposwarm results diff is-odd meshmart-catalog hl_overview`,
		Args: friendlyRangeArgs(2, 3, "reposwarm results diff <repo1> <repo2> [section]\n\nExamples:\n  reposwarm results diff is-odd meshmart-catalog\n  reposwarm results diff is-odd meshmart-catalog hl_overview"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			repo1, repo2 := args[0], args[1]

			if len(args) == 3 {
				section := args[2]
				var c1, c2 api.WikiContent
				if err := client.Get(ctx(), "/wiki/"+repo1+"/"+section, &c1); err != nil {
					return fmt.Errorf("reading %s/%s: %w", repo1, section, err)
				}
				if err := client.Get(ctx(), "/wiki/"+repo2+"/"+section, &c2); err != nil {
					return fmt.Errorf("reading %s/%s: %w", repo2, section, err)
				}

				if flagJSON {
					return output.JSON(map[string]any{
						"section":   section,
						"repo1":     repo1,
						"repo2":     repo2,
						"lines1":    len(strings.Split(c1.Content, "\n")),
						"lines2":    len(strings.Split(c2.Content, "\n")),
						"created1":  c1.CreatedAt,
						"created2":  c2.CreatedAt,
						"identical": c1.Content == c2.Content,
					})
				}

				lines1 := strings.Split(c1.Content, "\n")
				lines2 := strings.Split(c2.Content, "\n")

				F := output.F
				F.Section("Diff — " + section)
				F.KeyValue("A", fmt.Sprintf("%s (%d lines, %s)", repo1, len(lines1), c1.CreatedAt))
				F.KeyValue("B", fmt.Sprintf("%s (%d lines, %s)", repo2, len(lines2), c2.CreatedAt))
				F.Println()

				if c1.Content == c2.Content {
					F.Success("Sections are identical")
				} else {
					F.Info(fmt.Sprintf("Sections differ (%d vs %d lines)", len(lines1), len(lines2)))
				}
				F.Println()
				return nil
			}

			// Compare all sections
			var idx1, idx2 api.WikiIndex
			if err := client.Get(ctx(), "/wiki/"+repo1, &idx1); err != nil {
				return err
			}
			if err := client.Get(ctx(), "/wiki/"+repo2, &idx2); err != nil {
				return err
			}

			set1 := make(map[string]bool)
			for _, s := range idx1.Sections {
				set1[s.ID] = true
			}
			set2 := make(map[string]bool)
			for _, s := range idx2.Sections {
				set2[s.ID] = true
			}

			if flagJSON {
				only1, only2, both := diffSets(set1, set2)
				return output.JSON(map[string]any{
					"repo1":     repo1,
					"repo2":     repo2,
					"only1":     only1,
					"only2":     only2,
					"shared":    both,
					"sections1": len(idx1.Sections),
					"sections2": len(idx2.Sections),
				})
			}

			F := output.F
			F.Section("Investigation Comparison")
			F.KeyValue("A", fmt.Sprintf("%s (%d sections)", repo1, len(idx1.Sections)))
			F.KeyValue("B", fmt.Sprintf("%s (%d sections)", repo2, len(idx2.Sections)))
			F.Println()

			headers := []string{"Section", repo1, repo2}
			var rows [][]string

			allSections := make(map[string]bool)
			for k := range set1 {
				allSections[k] = true
			}
			for k := range set2 {
				allSections[k] = true
			}

			for s := range allSections {
				a, b := "-", "-"
				if set1[s] {
					a = "yes"
				}
				if set2[s] {
					b = "yes"
				}
				rows = append(rows, []string{s, a, b})
			}
			F.Table(headers, rows)
			F.Println()
			return nil
		},
	}
}

func diffSets(a, b map[string]bool) (only1, only2, both []string) {
	for k := range a {
		if b[k] {
			both = append(both, k)
		} else {
			only1 = append(only1, k)
		}
	}
	for k := range b {
		if !a[k] {
			only2 = append(only2, k)
		}
	}
	return
}
