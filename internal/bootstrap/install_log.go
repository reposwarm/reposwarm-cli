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
		return &InstallLog{logPath: logPath, started: time.Now()}
	}

	il := &InstallLog{file: f, logPath: logPath, started: time.Now()}
	il.writeHeader()
	return il
}

// Path returns the log file path.
func (il *InstallLog) Path() string { return il.logPath }

// Section writes a section header.
func (il *InstallLog) Section(title string) {
	sep := strings.Repeat("=", 60)
	il.line("\n" + sep)
	il.line("== " + title)
	il.line(sep)
	il.line("[" + time.Now().Format(time.RFC3339) + "]")
}

// Info logs an informational message.
func (il *InstallLog) Info(msg string) { il.line("[INFO]  " + msg) }

// Success logs a success message.
func (il *InstallLog) Success(msg string) { il.line("[OK]    " + msg) }

// Warning logs a warning.
func (il *InstallLog) Warning(msg string) { il.line("[WARN]  " + msg) }

// Error logs an error.
func (il *InstallLog) Error(msg string) { il.line("[ERROR] " + msg) }

// CmdOutput logs a command and its combined output.
func (il *InstallLog) CmdOutput(cmd string, dir string, output []byte, err error) {
	il.line("\n  $ " + cmd)
	if dir != "" {
		il.line("  (dir: " + dir + ")")
	}
	if len(output) > 0 {
		for _, l := range strings.Split(string(output), "\n") {
			il.line("  | " + l)
		}
	}
	if err != nil {
		il.line("  EXIT: " + err.Error())
	} else {
		il.line("  EXIT: 0")
	}
}

// Env logs environment variables (redacting secrets).
func (il *InstallLog) Env(env []string) {
	il.line("\n  Environment:")
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		key := parts[0]
		val := ""
		if len(parts) > 1 {
			val = parts[1]
		}
		lower := strings.ToLower(key)
		if strings.Contains(lower, "secret") || strings.Contains(lower, "password") ||
			strings.Contains(lower, "token") || strings.Contains(lower, "key") {
			if len(val) > 8 {
				val = val[:4] + "..." + val[len(val)-4:]
			} else {
				val = "***"
			}
		}
		il.line("    " + key + "=" + val)
	}
}

// Close writes the footer and closes the file.
func (il *InstallLog) Close() {
	elapsed := time.Since(il.started)
	sep := strings.Repeat("-", 60)
	il.line("\n" + sep)
	il.line("Total time: " + elapsed.Round(time.Second).String())
	il.line("Finished:   " + time.Now().Format(time.RFC3339))
	il.line(sep)
	if il.file != nil {
		il.file.Close()
	}
}

// RunCmd executes a command, logs it, and returns the output.
func (il *InstallLog) RunCmd(dir string, name string, args ...string) ([]byte, error) {
	cmdStr := name + " " + strings.Join(args, " ")
	il.line("\n  $ " + cmdStr)
	if dir != "" {
		il.line("  (dir: " + dir + ")")
	}

	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()

	if len(out) > 0 {
		for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			il.line("  | " + l)
		}
	}
	if err != nil {
		il.line("  EXIT: " + err.Error())
	} else {
		il.line("  EXIT: 0")
	}
	return out, err
}

func (il *InstallLog) writeHeader() {
	il.line("RepoSwarm Install Log")
	il.line(strings.Repeat("=", 60))
	il.line("Started:  " + il.started.Format(time.RFC3339))
	il.line(fmt.Sprintf("OS:       %s/%s", runtime.GOOS, runtime.GOARCH))

	if host, err := os.Hostname(); err == nil {
		il.line("Hostname: " + host)
	}
	if home, err := os.UserHomeDir(); err == nil {
		il.line("Home:     " + home)
	}
	if cwd, err := os.Getwd(); err == nil {
		il.line("CWD:      " + cwd)
	}
	if path := os.Getenv("PATH"); path != "" {
		paths := strings.Split(path, string(os.PathListSeparator))
		if len(paths) > 10 {
			il.line(fmt.Sprintf("PATH:     %s ... (%d entries)", strings.Join(paths[:5], string(os.PathListSeparator)), len(paths)))
		} else {
			il.line("PATH:     " + path)
		}
	}

	for _, tool := range []struct{ name, cmd string }{
		{"docker", "docker --version"},
		{"docker-compose", "docker compose version"},
		{"node", "node --version"},
		{"npm", "npm --version"},
		{"python", "python3 --version"},
		{"git", "git --version"},
	} {
		il.line(fmt.Sprintf("%-10s %s", tool.name+":", il.captureCmd(tool.cmd)))
	}
	il.line(strings.Repeat("=", 60))
}

func (il *InstallLog) captureCmd(cmd string) string {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "(empty)"
	}
	out, err := exec.Command(parts[0], parts[1:]...).CombinedOutput()
	if err != nil {
		return "(not found)"
	}
	return strings.TrimSpace(string(out))
}

// line writes a single line to the log file.
func (il *InstallLog) line(s string) {
	if il.file == nil {
		return
	}
	il.file.WriteString(s + "\n")
}
