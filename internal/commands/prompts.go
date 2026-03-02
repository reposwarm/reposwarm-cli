package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/loki-bedlam/reposwarm-cli/internal/api"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newPromptsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompts",
		Short: "Manage investigation prompts",
		Long: `View, edit, and manage the prompts used for architecture investigations.

Prompts are JSON-configured with markdown templates. Each prompt has a type
(base, type-specific, or detection) and can be toggled, reordered, or versioned.`,
	}
	cmd.AddCommand(newPromptsListCmd())
	cmd.AddCommand(newPromptsShowCmd())
	cmd.AddCommand(newPromptsCreateCmd())
	cmd.AddCommand(newPromptsUpdateCmd())
	cmd.AddCommand(newPromptsDeleteCmd())
	cmd.AddCommand(newPromptsToggleCmd())
	cmd.AddCommand(newPromptsOrderCmd())
	cmd.AddCommand(newPromptsContextCmd())
	cmd.AddCommand(newPromptsVersionsCmd())
	cmd.AddCommand(newPromptsRollbackCmd())
	cmd.AddCommand(newPromptsTypesCmd())
	cmd.AddCommand(newPromptsExportCmd())
	cmd.AddCommand(newPromptsImportCmd())
	return cmd
}

func newPromptsListCmd() *cobra.Command {
	var promptType string
	var enabledOnly, disabledOnly bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all prompts",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			path := "/prompts"
			if promptType != "" {
				path = "/prompts/types/" + promptType
			}

			var prompts []api.Prompt
			if err := client.Get(ctx(), path, &prompts); err != nil {
				return err
			}

			// If API has prompts, filter and show
			if len(prompts) > 0 {
				var filtered []api.Prompt
				for _, p := range prompts {
					if enabledOnly && !p.Enabled {
						continue
					}
					if disabledOnly && p.Enabled {
						continue
					}
					filtered = append(filtered, p)
				}
				if flagJSON {
					return output.JSON(filtered)
				}
				fmt.Printf("\n  %s (%d prompts)\n\n", output.Bold("Prompts"), len(filtered))
			headers := []string{"Name", "Type", "Enabled", "Order", "Version"}
			var rows [][]string
			for _, p := range filtered {
				en := "yes"
				if !p.Enabled {
					en = "no"
				}
				if output.IsHuman {
					if p.Enabled {
						en = output.Green("✓")
					} else {
						en = output.Dim("✗")
					}
				}
				rows = append(rows, []string{
					p.Name, p.Type, en,
					fmt.Sprint(p.Order), fmt.Sprintf("v%d", p.Version),
				})
			}
			output.Table(headers, rows)
				fmt.Println()
				return nil
			}

			// Fallback: derive sections from investigation results
			var repoList api.WikiReposResponse
			if err := client.Get(ctx(), "/wiki", &repoList); err != nil || len(repoList.Repos) == 0 {
				output.F.Info("No prompts configured and no results to derive from")
				return nil
			}

			// Get sections from first repo with results
			var index api.WikiIndex
			if err := client.Get(ctx(), "/wiki/"+repoList.Repos[0].Name, &index); err != nil {
				output.F.Info("No prompts configured")
				return nil
			}

			// Count frequency across sample
			sectionFreq := map[string]int{}
			sampleSize := len(repoList.Repos)
			if sampleSize > 5 {
				sampleSize = 5
			}
			for i := 0; i < sampleSize; i++ {
				var idx api.WikiIndex
				if err := client.Get(ctx(), "/wiki/"+repoList.Repos[i].Name, &idx); err != nil {
					continue
				}
				for _, s := range idx.Sections {
					sectionFreq[s.Name()]++
				}
			}

			if flagJSON {
				type ds struct {
					Name  string `json:"name"`
					Count int    `json:"frequency"`
				}
				var derived []ds
				for _, s := range index.Sections {
					derived = append(derived, ds{Name: s.Name(), Count: sectionFreq[s.Name()]})
				}
				return output.JSON(map[string]any{"source": "derived_from_results", "sections": derived})
			}

			F := output.F
			F.Section(fmt.Sprintf("Active Sections (%d, derived from results)", len(index.Sections)))
			F.Info("(Prompts API returned empty; showing sections found in results)")
			F.Println()
			headers := []string{"Section", "Found In"}
			var rows [][]string
			for _, s := range index.Sections {
				rows = append(rows, []string{
					s.Name(),
					fmt.Sprintf("%d/%d repos", sectionFreq[s.Name()], sampleSize),
				})
			}
			F.Table(headers, rows)
			F.Println()
			return nil
		},
	}

	cmd.Flags().StringVar(&promptType, "type", "", "Filter by prompt type")
	cmd.Flags().BoolVar(&enabledOnly, "enabled", false, "Show only enabled")
	cmd.Flags().BoolVar(&disabledOnly, "disabled", false, "Show only disabled")
	return cmd
}

func newPromptsShowCmd() *cobra.Command {
	var raw bool

	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show prompt details and template",
		Args:  friendlyExactArgs(1, "reposwarm prompts show <name>\n\nExample:\n  reposwarm prompts show hl_overview"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			var prompt api.Prompt
			if err := client.Get(ctx(), "/prompts/"+args[0], &prompt); err != nil {
				return err
			}

			if flagJSON {
				return output.JSON(prompt)
			}
			if raw {
				fmt.Print(prompt.Template)
				return nil
			}

			fmt.Printf("\n  %s\n\n", output.Bold("Prompt: "+prompt.Name))
			fmt.Printf("  %s  %s\n", output.Dim("Type       "), prompt.Type)
			fmt.Printf("  %s  %s\n", output.Dim("Description"), prompt.Description)
			fmt.Printf("  %s  %v\n", output.Dim("Enabled    "), prompt.Enabled)
			fmt.Printf("  %s  %d\n", output.Dim("Order      "), prompt.Order)
			fmt.Printf("  %s  v%d\n", output.Dim("Version    "), prompt.Version)
			if prompt.Context != "" {
				fmt.Printf("  %s  %s\n", output.Dim("Context    "), prompt.Context)
			}
			fmt.Printf("\n  %s\n\n%s\n", output.Bold("Template:"), prompt.Template)
			return nil
		},
	}

	cmd.Flags().BoolVar(&raw, "raw", false, "Output raw template only")
	return cmd
}

func newPromptsCreateCmd() *cobra.Command {
	var promptType, description, templateFile, template string
	var order int

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new prompt",
		Args:  friendlyExactArgs(1, "reposwarm prompts create <name> --template-file <file>\n\nExample:\n  reposwarm prompts create my_prompt --template-file prompt.md"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			tmpl := template
			if templateFile != "" {
				data, err := os.ReadFile(templateFile)
				if err != nil {
					return fmt.Errorf("reading template file: %w", err)
				}
				tmpl = string(data)
			}
			if tmpl == "" {
				return fmt.Errorf("provide --template or --template-file")
			}

			body := map[string]any{
				"name": args[0], "type": promptType,
				"description": description, "template": tmpl, "order": order,
			}

			var result any
			if err := client.Post(ctx(), "/prompts", body, &result); err != nil {
				return err
			}

			if flagJSON {
				return output.JSON(result)
			}
			output.Successf("Created prompt %s", output.Bold(args[0]))
			return nil
		},
	}

	cmd.Flags().StringVar(&promptType, "type", "base", "Prompt type")
	cmd.Flags().StringVar(&description, "description", "", "Description")
	cmd.Flags().StringVar(&template, "template", "", "Template content (inline)")
	cmd.Flags().StringVar(&templateFile, "template-file", "", "Template markdown file")
	cmd.Flags().IntVar(&order, "order", 0, "Execution order")
	return cmd
}

func newPromptsUpdateCmd() *cobra.Command {
	var description, templateFile, template string

	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a prompt",
		Args:  friendlyExactArgs(1, "reposwarm prompts update <name> [--template-file <file>]\n\nExample:\n  reposwarm prompts update hl_overview --template-file new.md"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			body := make(map[string]any)
			if description != "" {
				body["description"] = description
			}
			tmpl := template
			if templateFile != "" {
				data, err := os.ReadFile(templateFile)
				if err != nil {
					return fmt.Errorf("reading template: %w", err)
				}
				tmpl = string(data)
			}
			if tmpl != "" {
				body["template"] = tmpl
			}
			if len(body) == 0 {
				return fmt.Errorf("provide --template, --template-file, or --description")
			}

			var result any
			if err := client.Patch(ctx(), "/prompts/"+args[0], body, &result); err != nil {
				return err
			}
			if flagJSON {
				return output.JSON(result)
			}
			output.Successf("Updated prompt %s", output.Bold(args[0]))
			return nil
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "New description")
	cmd.Flags().StringVar(&template, "template", "", "New template (inline)")
	cmd.Flags().StringVar(&templateFile, "template-file", "", "Template file path")
	return cmd
}

func newPromptsDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a prompt",
		Args:  friendlyExactArgs(1, "reposwarm prompts delete <name>\n\nExample:\n  reposwarm prompts delete hl_overview"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				fmt.Printf("  Delete prompt %s? [y/N] ", output.Bold(args[0]))
				var confirm string
				fmt.Scanln(&confirm)
				if strings.ToLower(confirm) != "y" {
					output.Infof("Cancelled")
					return nil
				}
			}
			client, err := getClient()
			if err != nil {
				return err
			}
			var result any
			if err := client.Delete(ctx(), "/prompts/"+args[0], &result); err != nil {
				return err
			}
			if flagJSON {
				return output.JSON(map[string]any{"name": args[0], "deleted": true})
			}
			output.Successf("Deleted prompt %s", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation")
	return cmd
}

func newPromptsToggleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "toggle <name>",
		Short: "Toggle enabled/disabled",
		Args:  friendlyExactArgs(1, "reposwarm prompts toggle <name>\n\nExample:\n  reposwarm prompts toggle hl_overview"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}
			var result api.Prompt
			if err := client.Patch(ctx(), "/prompts/"+args[0]+"/toggle", nil, &result); err != nil {
				return err
			}
			if flagJSON {
				return output.JSON(map[string]any{"name": args[0], "enabled": result.Enabled})
			}
			state := output.Green("enabled")
			if !result.Enabled {
				state = output.Red("disabled")
			}
			output.Successf("Prompt %s is now %s", output.Bold(args[0]), state)
			return nil
		},
	}
}

func newPromptsOrderCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "order <name> <position>",
		Short: "Set execution order",
		Args:  friendlyExactArgs(2, "reposwarm prompts order <name> <position>\n\nExample:\n  reposwarm prompts order hl_overview 1"),
		RunE: func(cmd *cobra.Command, args []string) error {
			var order int
			if _, err := fmt.Sscanf(args[1], "%d", &order); err != nil {
				return fmt.Errorf("order must be a number")
			}
			client, err := getClient()
			if err != nil {
				return err
			}
			body := map[string]int{"order": order}
			var result any
			if err := client.Patch(ctx(), "/prompts/"+args[0]+"/order", body, &result); err != nil {
				return err
			}
			if flagJSON {
				return output.JSON(map[string]any{"name": args[0], "order": order})
			}
			output.Successf("Set %s order to %d", output.Bold(args[0]), order)
			return nil
		},
	}
}

func newPromptsContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "context <name> <text>",
		Short: "Set prompt context/instructions",
		Args:  friendlyExactArgs(2, "reposwarm prompts context <name> <text>\n\nExample:\n  reposwarm prompts context hl_overview \"Focus on architecture\""),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}
			body := map[string]string{"context": args[1]}
			var result any
			if err := client.Patch(ctx(), "/prompts/"+args[0]+"/context", body, &result); err != nil {
				return err
			}
			if flagJSON {
				return output.JSON(map[string]any{"name": args[0], "context": args[1]})
			}
			output.Successf("Updated context for %s", output.Bold(args[0]))
			return nil
		},
	}
}

func newPromptsVersionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "versions <name>",
		Short: "List version history",
		Args:  friendlyExactArgs(1, "reposwarm prompts versions <name>\n\nExample:\n  reposwarm prompts versions hl_overview"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}
			var versions []api.PromptVersion
			if err := client.Get(ctx(), "/prompts/"+args[0]+"/versions", &versions); err != nil {
				return err
			}
			if flagJSON {
				return output.JSON(versions)
			}
			fmt.Printf("\n  %s — %s (%d versions)\n\n",
				output.Bold("Version History"), output.Bold(args[0]), len(versions))
			headers := []string{"Version", "Created", "Author"}
			var rows [][]string
			for _, v := range versions {
				rows = append(rows, []string{fmt.Sprintf("v%d", v.Version), v.CreatedAt, v.Author})
			}
			output.Table(headers, rows)
			fmt.Println()
			return nil
		},
	}
}

func newPromptsRollbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rollback <name> <version>",
		Short: "Rollback to a specific version",
		Args:  friendlyExactArgs(2, "reposwarm prompts rollback <name> <version>\n\nExample:\n  reposwarm prompts rollback hl_overview 2"),
		RunE: func(cmd *cobra.Command, args []string) error {
			var ver int
			if _, err := fmt.Sscanf(args[1], "%d", &ver); err != nil {
				return fmt.Errorf("version must be a number")
			}
			client, err := getClient()
			if err != nil {
				return err
			}
			body := map[string]int{"version": ver}
			var result any
			if err := client.Post(ctx(), "/prompts/"+args[0]+"/rollback", body, &result); err != nil {
				return err
			}
			if flagJSON {
				return output.JSON(map[string]any{"name": args[0], "rolledBackTo": ver})
			}
			output.Successf("Rolled back %s to version %d", output.Bold(args[0]), ver)
			return nil
		},
	}
}

func newPromptsTypesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "types",
		Short: "List available prompt types",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}
			var types []api.PromptType
			if err := client.Get(ctx(), "/prompts/types", &types); err != nil {
				return err
			}
			if flagJSON {
				return output.JSON(types)
			}
			fmt.Printf("\n  %s\n\n", output.Bold("Prompt Types"))
			headers := []string{"Type", "Count"}
			var rows [][]string
			for _, pt := range types {
				rows = append(rows, []string{pt.Name, fmt.Sprint(pt.Count)})
			}
			output.Table(headers, rows)
			fmt.Println()
			return nil
		},
	}
}

func newPromptsExportCmd() *cobra.Command {
	var outputFile string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export all prompts as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}
			var result json.RawMessage
			if err := client.Post(ctx(), "/prompts/export", nil, &result); err != nil {
				return err
			}
			if outputFile != "" {
				data, _ := json.MarshalIndent(result, "", "  ")
				if err := os.WriteFile(outputFile, data, 0644); err != nil {
					return fmt.Errorf("writing file: %w", err)
				}
				output.Successf("Exported prompts to %s", outputFile)
				return nil
			}
			return output.JSON(result)
		},
	}
	cmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file path")
	return cmd
}

func newPromptsImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <file>",
		Short: "Import prompts from JSON file",
		Args:  friendlyExactArgs(1, "reposwarm prompts import <file>\n\nExample:\n  reposwarm prompts import prompts.json"),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("reading file: %w", err)
			}
			var body json.RawMessage
			if err := json.Unmarshal(data, &body); err != nil {
				return fmt.Errorf("invalid JSON: %w", err)
			}
			client, err := getClient()
			if err != nil {
				return err
			}
			var result any
			if err := client.Post(ctx(), "/prompts/import", body, &result); err != nil {
				return err
			}
			if flagJSON {
				return output.JSON(result)
			}
			output.Successf("Imported prompts from %s", args[0])
			return nil
		},
	}
}
