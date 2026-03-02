package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newResultsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "results",
		Aliases: []string{"res"},
		Short:   "Browse architecture investigation results",
	}
	cmd.AddCommand(newResultsListCmd())
	cmd.AddCommand(newResultsSectionsCmd())
	cmd.AddCommand(newResultsReadCmd())
	cmd.AddCommand(newResultsMetaCmd())
	cmd.AddCommand(newResultsExportCmd())
	cmd.AddCommand(newResultsSearchCmd())
	cmd.AddCommand(newResultsAuditCmd())
	cmd.AddCommand(newDiffCmd())
	cmd.AddCommand(newReportCmd())
	return cmd
}

func newResultsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List repos with investigation results",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			var result api.WikiReposResponse
			if err := client.Get(ctx(), "/wiki", &result); err != nil {
				return err
			}

			if flagJSON {
				return output.JSON(result.Repos)
			}

			F := output.F
			F.Section(fmt.Sprintf("Investigation Results (%d repos with results)", len(result.Repos)))
			headers := []string{"Repository", "Sections", "Last Updated"}
			var rows [][]string
			for _, r := range result.Repos {
				rows = append(rows, []string{
					r.Name,
					fmt.Sprint(r.SectionCount),
					r.LastUpdated,
				})
			}
			F.Table(headers, rows)
			F.Println()
			return nil
		},
	}
}

func newResultsSectionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "sections <repo>",
		Aliases: []string{"show"},
		Short:   "List investigation sections for a repo",
		Args:    friendlyExactArgs(1, "reposwarm results sections <repo>\n\nExample:\n  reposwarm results sections my-repo"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			var index api.WikiIndex
			if err := client.Get(ctx(), "/wiki/"+args[0], &index); err != nil {
				return err
			}

			if flagJSON {
				return output.JSON(index)
			}

			F := output.F
			F.Section(fmt.Sprintf("Results — %s (%d sections)", args[0], len(index.Sections)))
			headers := []string{"Section", "Created"}
			var rows [][]string
			for _, s := range index.Sections {
				rows = append(rows, []string{
					F.SectionIcon(s.Name()) + s.Name(),
					s.CreatedAt,
				})
			}
			F.Table(headers, rows)
			F.Println()
			return nil
		},
	}
}

func newResultsReadCmd() *cobra.Command {
	var raw bool

	cmd := &cobra.Command{
		Use:   "read <repo> [section]",
		Short: "Read investigation results (one section or all)",
		Long: `Read investigation results for a repository.

With section name: returns just that section.
Without section name: returns ALL sections concatenated.

Examples:
  reposwarm results read is-odd                  # All sections
  reposwarm results read is-odd hl_overview      # Single section
  reposwarm results read is-odd --raw > out.md   # Raw markdown`,
		Args: friendlyRangeArgs(1, 2, "reposwarm results read <repo> [section]\n\nExamples:\n  reposwarm results read my-repo\n  reposwarm results read my-repo hl_overview"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			repo := args[0]

			if len(args) == 2 {
				section := args[1]
				var content api.WikiContent
				if err := client.Get(ctx(), "/wiki/"+repo+"/"+section, &content); err != nil {
					return err
				}

				if flagJSON {
					return output.JSON(content)
				}
				if raw {
					fmt.Print(content.Content)
					return nil
				}
				F := output.F
				F.Section(fmt.Sprintf("Results — %s / %s", repo, section))
				F.Info(content.CreatedAt)
				F.Println()
				fmt.Println(content.Content)
				return nil
			}

			// All sections
			var index api.WikiIndex
			if err := client.Get(ctx(), "/wiki/"+repo, &index); err != nil {
				return err
			}

			if len(index.Sections) == 0 {
				return fmt.Errorf("no investigation results for %s", repo)
			}

			var allContent []api.WikiContent
			for _, s := range index.Sections {
				var content api.WikiContent
				if err := client.Get(ctx(), "/wiki/"+repo+"/"+s.Name(), &content); err != nil {
					output.F.Error(fmt.Sprintf("Failed to read %s: %s", s.Name(), err))
					continue
				}
				allContent = append(allContent, content)
			}

			if flagJSON {
				return output.JSON(allContent)
			}

			if raw {
				for _, c := range allContent {
					fmt.Printf("## %s\n\n%s\n\n", c.Section, c.Content)
				}
				return nil
			}

			F := output.F
			F.Section(fmt.Sprintf("Full Investigation — %s (%d sections)", repo, len(allContent)))
			for _, c := range allContent {
				F.Printf("--- %s ---\n", c.Section)
				F.Info(c.CreatedAt)
				F.Println()
				fmt.Println(c.Content)
				F.Println()
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&raw, "raw", false, "Output raw markdown (no formatting)")
	return cmd
}

func newResultsMetaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "meta <repo> [section]",
		Short: "Show metadata for investigation results (no content)",
		Args:  friendlyRangeArgs(1, 2, "reposwarm results meta <repo> [section]\n\nExamples:\n  reposwarm results meta my-repo\n  reposwarm results meta my-repo hl_overview"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			repo := args[0]

			if len(args) == 2 {
				section := args[1]
				var content api.WikiContent
				if err := client.Get(ctx(), "/wiki/"+repo+"/"+section, &content); err != nil {
					return err
				}

				meta := map[string]any{
					"repo":         content.Repo,
					"section":      content.Section,
					"createdAt":    content.CreatedAt,
					"timestamp":    content.Timestamp,
					"referenceKey": content.ReferenceKey,
				}

				if flagJSON {
					return output.JSON(meta)
				}

				F := output.F
				F.Section("Section Metadata")
				F.KeyValue("Repository", repo)
				F.KeyValue("Section", section)
				F.KeyValue("Created", content.CreatedAt)
				F.KeyValue("Timestamp", fmt.Sprint(content.Timestamp))
				F.KeyValue("Ref Key", content.ReferenceKey)
				F.Println()
				return nil
			}

			// Repo-level metadata
			var index api.WikiIndex
			if err := client.Get(ctx(), "/wiki/"+repo, &index); err != nil {
				return err
			}

			meta := map[string]any{
				"repo":     repo,
				"sections": len(index.Sections),
				"hasDocs":  index.HasDocs,
			}
			if len(index.Sections) > 0 {
				meta["lastSection"] = index.Sections[len(index.Sections)-1].CreatedAt
			}

			if flagJSON {
				return output.JSON(meta)
			}

			F := output.F
			F.Section("Repository Metadata")
			F.KeyValue("Repository", repo)
			F.KeyValue("Sections", fmt.Sprint(len(index.Sections)))
			F.KeyValue("Has Docs", fmt.Sprint(index.HasDocs))
			if len(index.Sections) > 0 {
				F.KeyValue("Last Update", index.Sections[len(index.Sections)-1].CreatedAt)
			}
			F.Println()
			return nil
		},
	}
}

func newResultsExportCmd() *cobra.Command {
	var outputFile string
	var outputDir string
	var all bool

	cmd := &cobra.Command{
		Use:   "export [repo]",
		Short: "Export investigation results as markdown",
		Long: `Export investigation results to local markdown files.

Single repo:
  reposwarm results export my-app              # stdout
  reposwarm results export my-app -o out.md    # specific file
  reposwarm results export my-app -d ./docs    # writes docs/my-app.arch.md

All repos:
  reposwarm results export --all -d ./arch-docs      # exports all repos to directory`,
		Args: friendlyMaxArgs(1, "reposwarm results export [repo] [--all]\n\nExamples:\n  reposwarm results export my-repo\n  reposwarm results export --all -d ./docs"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			if all {
				if outputDir == "" {
					outputDir = "."
				}
				return exportAllRepos(client, outputDir)
			}

			if len(args) == 0 {
				return fmt.Errorf("provide a repo name or use --all")
			}

			repo := args[0]
			md, sections, err := exportRepo(client, repo)
			if err != nil {
				return err
			}

			// Determine output path
			dest := outputFile
			if dest == "" && outputDir != "" {
				dest = fmt.Sprintf("%s/%s.arch.md", outputDir, repo)
			}

			if dest != "" {
				if outputDir != "" {
					os.MkdirAll(outputDir, 0755)
				}
				if err := os.WriteFile(dest, []byte(md), 0644); err != nil {
					return fmt.Errorf("writing file: %w", err)
				}
				output.F.Success(fmt.Sprintf("Exported %d sections to %s (%d bytes)", sections, dest, len(md)))
				return nil
			}

			fmt.Print(md)
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file path")
	cmd.Flags().StringVarP(&outputDir, "dir", "d", "", "Output directory (writes <repo>.arch.md)")
	cmd.Flags().BoolVar(&all, "all", false, "Export all repos")
	return cmd
}

func exportRepo(client *api.Client, repo string) (string, int, error) {
	var index api.WikiIndex
	if err := client.Get(ctx(), "/wiki/"+repo, &index); err != nil {
		return "", 0, err
	}

	var sb strings.Builder
	for _, s := range index.Sections {
		var content api.WikiContent
		if err := client.Get(ctx(), "/wiki/"+repo+"/"+s.Name(), &content); err != nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("# %s\n%s\n", s.Name(), content.Content))
	}

	return sb.String(), len(index.Sections), nil
}

func exportAllRepos(client *api.Client, dir string) error {
	var repoList api.WikiReposResponse
	if err := client.Get(ctx(), "/wiki", &repoList); err != nil {
		return err
	}

	os.MkdirAll(dir, 0755)

	exported := 0
	for _, r := range repoList.Repos {
		md, sections, err := exportRepo(client, r.Name)
		if err != nil {
			output.F.Error(fmt.Sprintf("Failed to export %s: %s", r.Name, err))
			continue
		}
		dest := fmt.Sprintf("%s/%s.arch.md", dir, r.Name)
		if err := os.WriteFile(dest, []byte(md), 0644); err != nil {
			output.F.Error(fmt.Sprintf("Failed to write %s: %s", dest, err))
			continue
		}
		output.F.Success(fmt.Sprintf("%s (%d sections, %d bytes)", r.Name, sections, len(md)))
		exported++
	}

	output.F.Println()
	output.F.Success(fmt.Sprintf("Exported %d/%d repos to %s", exported, len(repoList.Repos), dir))
	return nil
}


