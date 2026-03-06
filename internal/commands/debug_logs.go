package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

const debugLogsMaxAgentChars = 5000

func newDebugLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug-logs",
		Short: "Show install/setup debug logs",
		Long: `Display the most recent install/setup log file.

For humans: output is paged (less/more).
For agents (--for-agent): full text output, truncated at 5000 chars with file path link.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			installDir := cfg.EffectiveInstallDir()
			logsDir := filepath.Join(installDir, "logs")

			// Find the most recent install log
			logFile, err := findLatestLog(logsDir)
			if err != nil {
				return fmt.Errorf("no install logs found at %s\nRun 'reposwarm new --local' first", logsDir)
			}

			content, err := os.ReadFile(logFile)
			if err != nil {
				return fmt.Errorf("reading log: %w", err)
			}

			if flagAgent || flagJSON {
				// Agent mode: dump text, truncate if needed
				text := string(content)
				if len(text) > debugLogsMaxAgentChars {
					fmt.Println(text[:debugLogsMaxAgentChars])
					fmt.Printf("\n... (truncated, %d total chars)\n", len(text))
					fmt.Printf("Full log: %s\n", logFile)
				} else {
					fmt.Println(text)
					fmt.Printf("Log file: %s\n", logFile)
				}
				return nil
			}

			// Human mode: use pager
			return pageContent(string(content), logFile)
		},
	}
	return cmd
}

// findLatestLog finds the most recent install-*.log file in the logs directory.
func findLatestLog(logsDir string) (string, error) {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return "", err
	}

	var logFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "install-") && strings.HasSuffix(e.Name(), ".log") {
			logFiles = append(logFiles, filepath.Join(logsDir, e.Name()))
		}
	}

	if len(logFiles) == 0 {
		return "", fmt.Errorf("no log files found")
	}

	sort.Strings(logFiles)
	return logFiles[len(logFiles)-1], nil // Latest by timestamp in filename
}

// pageContent displays content using a pager (less, more, or direct output).
func pageContent(content string, title string) error {
	output.F.Info(fmt.Sprintf("Log file: %s", title))
	fmt.Println()

	// Try to use less first, fall back to more, then direct
	for _, pager := range []string{"less", "more"} {
		pagerPath, err := findInPath(pager)
		if err != nil {
			continue
		}

		cmd := exec.Command(pagerPath)
		cmd.Stdin = strings.NewReader(content)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// No pager available, print directly
	fmt.Println(content)
	return nil
}

func findInPath(name string) (string, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("not found")
}
