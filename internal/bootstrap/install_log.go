package bootstrap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// InstallLog captures detailed install/setup information to a file.
// It writes everything needed to debug issues remotely.
type InstallLog struct {
	file    *os.File
	logPath string
	started time.Time
}

// NewInstallLog creates a new install log in the given directory.
// Creates the directory if needed. Returns nil (no-op) if it can't create the file.
func NewInstallLog(installDir string) *InstallLog {
	logsDir := filepath.Join(installDir, "logs")
	os.MkdirAll(logsDir, 0755)

	timestamp := time.Now().Format("20060102-150405")
	logPath := filepath.Join(logsDir, fmt.Sprintf("install-%s.log", timestamp))

	f, err := os.Create(logPath)
	if err != nil {
		// Can't create log — return no-op logger
		return &InstallLog{logPath: logPath, started: time.Now()}
	}

	il := &InstallLog{
		file:    f,
		logPath: logPath,
		started: time.Now(),
	}

	// Write header with environment info
	il.writeHeader()
	return il
}

// Path returns the log file path.
func (il *InstallLog) Path() string {
	return il.logPath
}

// Section writes a section header.
func (il *InstallLog) Section(title string) {
	il.writef("\n" + strings.Repeat("=", 60) + "\n")
	il.writef("== %s\n", title)
	il.writef(strings.Repeat("=", 60) + "\n")
	il.writef("[%s]\n", time.Now().Format(time.RFC3339))
}

// Info logs an informational message.
func (il *InstallLog) Info(msg string) {
	il.writef("[INFO]  %s\n", msg)
}

// Success logs a success message.
func (il *InstallLog) Success(msg string) {
	il.writef("[OK]    %s\n", msg)
}

// Warning logs a warning.
func (il *InstallLog) Warning(msg string) {
	il.writef("[WARN]  %s\n", msg)
}

// Error logs an error.
func (il *InstallLog) Error(msg string) {
	il.writef("[ERROR] %s\n", msg)
}

// CmdOutput logs a command and its combined output.
func (il *InstallLog) CmdOutput(cmd string, dir string, output []byte, err error) {
	il.writef("\n  $ %s\n", cmd)
	if dir != "" {
		il.writef("  (dir: %s)\n", dir)
	}
	if len(output) > 0 {
		for _, line := range strings.Split(string(output), "\n") {
			il.writef("  | %s\n", line)
		}
	}
	if err != nil {
		il.writef("  EXIT: %v\n", err)
	} else {
		il.writef("  EXIT: 0\n")
	}
}

// Env logs environment variables (redacting secrets).
func (il *InstallLog) Env(env []string) {
	il.writef("\n  Environment:\n")
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		key := parts[0]
		val := ""
		if len(parts) > 1 {
			val = parts[1]
		}
		// Redact secrets
		lower := strings.ToLower(key)
		if strings.Contains(lower, "secret") || strings.Contains(lower, "password") ||
			strings.Contains(lower, "token") || strings.Contains(lower, "key") {
			if len(val) > 8 {
				val = val[:4] + "..." + val[len(val)-4:]
			} else {
				val = "***"
			}
		}
		il.writef("    %s=%s\n", key, val)
	}
}

// Close writes the footer and closes the file.
func (il *InstallLog) Close() {
	elapsed := time.Since(il.started)
	il.writef("\n" + strings.Repeat("-", 60) + "\n")
	il.writef("Total time: %s\n", elapsed.Round(time.Second))
	il.writef("Finished:   %s\n", time.Now().Format(time.RFC3339))
	il.writef(strings.Repeat("-", 60) + "\n")
	if il.file != nil {
		il.file.Close()
	}
}

func (il *InstallLog) writeHeader() {
	il.writef("RepoSwarm Install Log\n")
	il.writef(strings.Repeat("=", 60) + "\n")
	il.writef("Started:  %s\n", il.started.Format(time.RFC3339))
	il.writef("OS:       %s/%s\n", runtime.GOOS, runtime.GOARCH)

	// Hostname
	if host, err := os.Hostname(); err == nil {
		il.writef("Hostname: %s\n", host)
	}

	// User
	if home, err := os.UserHomeDir(); err == nil {
		il.writef("Home:     %s\n", home)
	}

	// CWD
	if cwd, err := os.Getwd(); err == nil {
		il.writef("CWD:      %s\n", cwd)
	}

	// PATH (abbreviated)
	if path := os.Getenv("PATH"); path != "" {
		paths := strings.Split(path, string(os.PathListSeparator))
		if len(paths) > 10 {
			il.writef("PATH:     %s ... (%d entries)\n", strings.Join(paths[:5], string(os.PathListSeparator)), len(paths))
		} else {
			il.writef("PATH:     %s\n", path)
		}
	}

	// Key tool versions
	for _, tool := range []struct{ name, cmd string }{
		{"docker", "docker --version"},
		{"docker-compose", "docker compose version"},
		{"node", "node --version"},
		{"npm", "npm --version"},
		{"python", "python3 --version"},
		{"git", "git --version"},
	} {
		il.writef("%-10s %s\n", tool.name+":", il.captureCmd(tool.cmd))
	}

	il.writef(strings.Repeat("=", 60) + "\n")
}

func (il *InstallLog) captureCmd(cmd string) string {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "(empty)"
	}
	out, err := captureCommandOutput(parts[0], parts[1:]...)
	if err != nil {
		return fmt.Sprintf("(not found: %v)", err)
	}
	return strings.TrimSpace(string(out))
}

func (il *InstallLog) writef(format string, args ...any) {
	if il.file == nil {
		return
	}
	fmt.Fprintf(il.file, format, args...)
}

func captureCommandOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

// RunCmd executes a command, logs it, and returns the output.
// This is the preferred way to run commands during install — everything gets logged.
func (il *InstallLog) RunCmd(dir string, name string, args ...string) ([]byte, error) {
	cmdStr := name + " " + strings.Join(args, " ")
	il.writef("\n  $ %s\n", cmdStr)
	if dir != "" {
		il.writef("  (dir: %s)\n", dir)
	}

	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()

	if len(out) > 0 {
		lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		for _, line := range lines {
			il.writef("  | %s\n", line)
		}
	}
	if err != nil {
		il.writef("  EXIT: %v\n", err)
	} else {
		il.writef("  EXIT: 0\n")
	}
	return out, err
}
