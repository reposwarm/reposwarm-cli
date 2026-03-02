package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// friendlyExactArgs returns a PositionalArgs that shows a friendly message when
// the wrong number of arguments is provided.
func friendlyExactArgs(n int, usage string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == n {
			return nil
		}
		if len(args) == 0 {
			return fmt.Errorf("💡 %s", usage)
		}
		if len(args) > n {
			return fmt.Errorf("💡 Too many arguments (expected %d, got %d).\n\n%s", n, len(args), usage)
		}
		return fmt.Errorf("💡 Not enough arguments (expected %d, got %d).\n\n%s", n, len(args), usage)
	}
}

// friendlyRangeArgs returns a PositionalArgs for a range of expected arguments.
func friendlyRangeArgs(min, max int, usage string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) >= min && len(args) <= max {
			return nil
		}
		if len(args) == 0 {
			return fmt.Errorf("💡 %s", usage)
		}
		if len(args) > max {
			return fmt.Errorf("💡 Too many arguments (expected %d–%d, got %d).\n\n%s", min, max, len(args), usage)
		}
		return fmt.Errorf("💡 Not enough arguments (expected %d–%d, got %d).\n\n%s", min, max, len(args), usage)
	}
}

// friendlyMaxArgs returns a PositionalArgs that allows up to max arguments.
func friendlyMaxArgs(max int, usage string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) <= max {
			return nil
		}
		return fmt.Errorf("💡 Too many arguments (expected at most %d, got %d).\n\n%s", max, len(args), usage)
	}
}
