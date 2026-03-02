package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/loki-bedlam/reposwarm-cli/internal/bootstrap"
	"github.com/loki-bedlam/reposwarm-cli/internal/config"
	"github.com/loki-bedlam/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	serviceColors = map[string]func(a ...interface{}) string{
		"api":      color.New(color.FgBlue).SprintFunc(),
		"worker":   color.New(color.FgGreen).SprintFunc(),
		"temporal": color.New(color.FgCyan).SprintFunc(),
		"ui":       color.New(color.FgMagenta).SprintFunc(),
	}
)

func newLogsCmd() *cobra.Command {
	var (
		tail  bool
		lines int
	)

	cmd := &cobra.Command{
		Use:   "logs [service]",
		Short: "Show/tail logs from local services",
		Long: `Show or tail logs from local RepoSwarm services.

Available services: api, worker, temporal, ui

If no service is specified, shows logs from all services.
This command only works with local RepoSwarm installations.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get install directory
			env := bootstrap.Detect()
			installDir := env.InstallDir()

			// Check for ~/.reposwarm/local as fallback
			cfg, _ := config.Load()
			if cfg != nil && cfg.APIUrl != "" && strings.Contains(cfg.APIUrl, "localhost") {
				// Likely a local install, check default location
				homeDir, _ := os.UserHomeDir()
				altDir := filepath.Join(homeDir, ".reposwarm", "local")
				if _, err := os.Stat(filepath.Join(altDir, "logs")); err == nil {
					installDir = altDir
				}
			}

			logsDir := filepath.Join(installDir, "logs")
			if _, err := os.Stat(logsDir); os.IsNotExist(err) {
				return fmt.Errorf("no local installation found (checked %s)", logsDir)
			}

			// Determine which services to show
			services := []string{"api", "worker", "temporal", "ui"}
			if len(args) > 0 {
				service := args[0]
				// Validate service name
				valid := false
				for _, s := range services {
					if s == service {
						valid = true
						break
					}
				}
				if !valid {
					return fmt.Errorf("invalid service: %s (must be one of: api, worker, temporal, ui)", service)
				}
				services = []string{service}
			}

			// Check which log files exist
			var existingServices []string
			for _, service := range services {
				logFile := filepath.Join(logsDir, service+".log")
				if _, err := os.Stat(logFile); err == nil {
					existingServices = append(existingServices, service)
				}
			}

			if len(existingServices) == 0 {
				output.F.Warning("No log files found. Services may not be running.")
				output.F.Info(fmt.Sprintf("Checked directory: %s", logsDir))
				return nil
			}

			// Handle tail mode
			if tail {
				return tailLogs(logsDir, existingServices)
			}

			// Read last N lines from each service
			for _, service := range existingServices {
				logFile := filepath.Join(logsDir, service+".log")
				if err := showLogFile(service, logFile, lines); err != nil {
					output.F.Warning(fmt.Sprintf("Error reading %s logs: %v", service, err))
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&tail, "tail", "f", false, "Follow/stream logs")
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "Number of lines to show")

	return cmd
}

func showLogFile(service, logFile string, maxLines int) error {
	file, err := os.Open(logFile)
	if err != nil {
		return err
	}
	defer file.Close()

	// Read all lines
	var allLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}

	// Get last N lines
	start := 0
	if len(allLines) > maxLines && maxLines > 0 {
		start = len(allLines) - maxLines
	}

	F := output.F
	colorFunc := serviceColors[service]
	if colorFunc == nil {
		colorFunc = fmt.Sprint
	}

	F.Section(colorFunc(fmt.Sprintf("%s logs", strings.ToUpper(service))))
	for i := start; i < len(allLines); i++ {
		if flagJSON {
			// Parse timestamp if possible and output as JSON
			line := allLines[i]
			timestamp := ""
			// Try to extract timestamp (common formats)
			parts := strings.SplitN(line, " ", 3)
			if len(parts) >= 2 {
				// Try parsing first two parts as timestamp
				if t, err := time.Parse("2006-01-02 15:04:05", parts[0]+" "+parts[1]); err == nil {
					timestamp = t.Format(time.RFC3339)
				}
			}
			jsonLine := map[string]string{
				"service":   service,
				"line":      line,
				"timestamp": timestamp,
			}
			data, _ := json.Marshal(jsonLine)
			fmt.Println(string(data))
		} else {
			F.Println(allLines[i])
		}
	}
	F.Println()

	return nil
}

func tailLogs(logsDir string, services []string) error {
	F := output.F
	F.Info(fmt.Sprintf("Tailing logs from: %s", strings.Join(services, ", ")))
	F.Info("Press Ctrl+C to stop")
	F.Println()

	// Open all log files
	type logReader struct {
		service string
		file    *os.File
		reader  *bufio.Reader
	}

	var readers []logReader
	for _, service := range services {
		logFile := filepath.Join(logsDir, service+".log")
		file, err := os.Open(logFile)
		if err != nil {
			continue
		}
		defer file.Close()

		// Seek to end of file
		file.Seek(0, io.SeekEnd)
		readers = append(readers, logReader{
			service: service,
			file:    file,
			reader:  bufio.NewReader(file),
		})
	}

	// Poll for new lines
	for {
		hasNewContent := false
		for _, lr := range readers {
			for {
				line, err := lr.reader.ReadString('\n')
				if err != nil {
					break // No more lines available
				}
				hasNewContent = true

				// Format and print the line
				colorFunc := serviceColors[lr.service]
				if colorFunc == nil {
					colorFunc = fmt.Sprint
				}

				if flagJSON {
					timestamp := ""
					// Try to extract timestamp
					parts := strings.SplitN(strings.TrimSpace(line), " ", 3)
					if len(parts) >= 2 {
						if t, err := time.Parse("2006-01-02 15:04:05", parts[0]+" "+parts[1]); err == nil {
							timestamp = t.Format(time.RFC3339)
						}
					}
					jsonLine := map[string]string{
						"service":   lr.service,
						"line":      strings.TrimSpace(line),
						"timestamp": timestamp,
					}
					data, _ := json.Marshal(jsonLine)
					fmt.Println(string(data))
				} else {
					F.Printf("[%s] %s", colorFunc(lr.service), line)
				}
			}
		}

		if !hasNewContent {
			time.Sleep(500 * time.Millisecond)
		}
	}
}

// logEntry represents a single log line with metadata for sorting
type logEntry struct {
	service   string
	timestamp time.Time
	line      string
}

func parseLogTimestamp(line string) (time.Time, bool) {
	// Try common timestamp formats
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
	}

	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return time.Time{}, false
	}

	timeStr := parts[0] + " " + parts[1]
	for _, format := range formats {
		if t, err := time.Parse(format, timeStr); err == nil {
			return t, true
		}
	}

	// Try just the first part for ISO format
	for _, format := range formats {
		if t, err := time.Parse(format, parts[0]); err == nil {
			return t, true
		}
	}

	return time.Time{}, false
}

// interleaveLogs combines logs from multiple services sorted by timestamp
func interleaveLogs(logsDir string, services []string, maxLines int) error {
	var allEntries []logEntry

	// Read all log files
	for _, service := range services {
		logFile := filepath.Join(logsDir, service+".log")
		file, err := os.Open(logFile)
		if err != nil {
			continue
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			entry := logEntry{
				service: service,
				line:    line,
			}

			// Try to parse timestamp
			if t, ok := parseLogTimestamp(line); ok {
				entry.timestamp = t
			}

			allEntries = append(allEntries, entry)
		}
	}

	// Sort by timestamp
	sort.Slice(allEntries, func(i, j int) bool {
		// If both have timestamps, sort by time
		if !allEntries[i].timestamp.IsZero() && !allEntries[j].timestamp.IsZero() {
			return allEntries[i].timestamp.Before(allEntries[j].timestamp)
		}
		// Otherwise maintain original order
		return false
	})

	// Get last N entries
	start := 0
	if len(allEntries) > maxLines && maxLines > 0 {
		start = len(allEntries) - maxLines
	}

	F := output.F
	F.Section("Interleaved Logs")
	for i := start; i < len(allEntries); i++ {
		entry := allEntries[i]
		colorFunc := serviceColors[entry.service]
		if colorFunc == nil {
			colorFunc = fmt.Sprint
		}

		if flagJSON {
			timestamp := ""
			if !entry.timestamp.IsZero() {
				timestamp = entry.timestamp.Format(time.RFC3339)
			}
			jsonLine := map[string]string{
				"service":   entry.service,
				"line":      entry.line,
				"timestamp": timestamp,
			}
			data, _ := json.Marshal(jsonLine)
			fmt.Println(string(data))
		} else {
			F.Printf("[%s] %s\n", colorFunc(entry.service), entry.line)
		}
	}

	return nil
}