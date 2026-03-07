package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/reposwarm/reposwarm-cli/internal/api"
	"github.com/reposwarm/reposwarm-cli/internal/config"
	"github.com/reposwarm/reposwarm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newAskCmd() *cobra.Command {
	var archFlag bool
	var localFlag bool
	var reposFlag string
	var adapterFlag string
	var noWaitFlag bool
	var hubURLFlag string
	var modelFlag string

	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Ask the RepoSwarm AI assistant a question",
		Long: `Ask a question about RepoSwarm or your architecture.

Without --arch: asks about RepoSwarm CLI usage (fast, simple Q&A).
With --arch:    queries your architecture docs using the askbox agent.

Modes:
  --arch              Use the askbox agent for architecture analysis
  --arch --local      Run askbox via Docker locally (no API server needed)
  --arch              Route through the API server (default when connected)

Flags:
  --repos <list>      Comma-separated repos to scope the question to
  --adapter <name>    Agent adapter: claude-agent-sdk (default) or strands
  --model <id>        Override LLM model
  --hub-url <url>     Arch-hub git URL (or set archHubUrl in config)
  --no-wait           Return ask-id immediately without waiting for answer
  --local             Run askbox container directly via Docker

Output modes:
  (default)           Human-friendly with progress indicators
  --for-agent         Plain text answer only, no formatting
  --json              Structured JSON output

Examples:
  reposwarm ask "how do I add a new repo?"
  reposwarm ask --arch "how does auth work across all services?"
  reposwarm ask --arch --local "what databases are used?"
  reposwarm ask --arch --local --hub-url https://github.com/org/arch-hub.git "summarize auth"
  reposwarm ask --arch --repos my-api,billing "how do they communicate?"
  reposwarm ask --arch --adapter strands "what patterns exist?"
  reposwarm ask --arch --no-wait --json "what patterns do repos share?"
  reposwarm ask --arch --for-agent "summarize the test strategies"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			question := strings.Join(args, " ")

			// Check if standalone ask CLI is installed — prefer it for all queries
			askPath, askErr := exec.LookPath("ask")
			if askErr == nil {
				// ask CLI found — forward to it
				if !flagJSON && !flagAgent {
					fmt.Fprintf(os.Stderr, "💡 Forwarding to ask CLI — you can use it directly next time: ask %q\n\n", question)
				}
				askCmd := exec.Command(askPath, question)
				askCmd.Stdout = os.Stdout
				askCmd.Stderr = os.Stderr
				askCmd.Stdin = os.Stdin
				return askCmd.Run()
			}

			if archFlag {
				// Deprecation notice
				if !flagJSON && !flagAgent {
					fmt.Fprintf(os.Stderr, "💡 Tip: Architecture queries are moving to the standalone `ask` CLI.\n")
					fmt.Fprintf(os.Stderr, "   Install: curl -fsSL https://raw.githubusercontent.com/reposwarm/ask-cli/main/install.sh | sh\n")
					fmt.Fprintf(os.Stderr, "   Usage:   ask %q\n\n", question)
				}

				// For arch questions, try local askbox server first (fast path)
				if localFlag {
					return runLocalArchAsk(question, hubURLFlag, reposFlag, adapterFlag, modelFlag)
				}

				// Try local askbox first if it's a local install
				cfg, _ := config.Load()
				if cfg != nil && cfg.InstallType == "docker" {
					if err := tryAskboxServer("http://localhost:8082", question, reposFlag, adapterFlag, modelFlag); err == nil {
						return nil
					}
				}

				// Fall back to API server
				client, err := getClient()
				if err != nil {
					if flagJSON {
						return output.JSON(map[string]any{
							"success": false,
							"error":   "cannot connect to API or askbox server",
							"hint":    "use --local to run askbox via Docker",
						})
					}
					return fmt.Errorf("cannot connect to API or askbox server\n  💡 Use --local to run askbox via Docker")
				}
				return runArchAsk(client, question, reposFlag, adapterFlag, noWaitFlag)
			}

			client, err := getClient()
			if err != nil {
				// API unreachable and no ask CLI — offer to install
				if !flagJSON && !flagAgent {
					fmt.Fprintf(os.Stderr, "❌ Cannot connect to API server.\n\n")
					fmt.Fprintf(os.Stderr, "💡 Install the standalone ask CLI for architecture queries:\n")
					fmt.Fprintf(os.Stderr, "   curl -fsSL https://raw.githubusercontent.com/reposwarm/ask-cli/main/install.sh | sh\n")
					fmt.Fprintf(os.Stderr, "   ask setup    # Configure provider & start askbox\n")
					fmt.Fprintf(os.Stderr, "   ask %q\n", question)

					reader := bufio.NewReader(os.Stdin)
					fmt.Fprintf(os.Stderr, "\n Install ask CLI now? [Y/n]: ")
					answer, _ := reader.ReadString('\n')
					answer = strings.TrimSpace(strings.ToLower(answer))
					if answer == "" || answer == "y" || answer == "yes" {
						installCmd := exec.Command("sh", "-c", "curl -fsSL https://raw.githubusercontent.com/reposwarm/ask-cli/main/install.sh | sh")
						installCmd.Stdout = os.Stdout
						installCmd.Stderr = os.Stderr
						if iErr := installCmd.Run(); iErr != nil {
							return fmt.Errorf("install failed: %w", iErr)
						}
						fmt.Fprintf(os.Stderr, "\n✅ Installed! Run: ask setup\n")
						return nil
					}
				}
				return err
			}

			return runSimpleAsk(client, question)
		},
	}

	cmd.Flags().BoolVar(&archFlag, "arch", false, "Query architecture docs using the askbox agent")
	cmd.Flags().BoolVar(&localFlag, "local", false, "Run askbox container directly via Docker (no API needed)")
	cmd.Flags().StringVar(&reposFlag, "repos", "", "Comma-separated list of repos to scope the question to")
	cmd.Flags().StringVar(&adapterFlag, "adapter", "", "Agent adapter: claude-agent-sdk (default) or strands")
	cmd.Flags().StringVar(&modelFlag, "model", "", "Override LLM model ID")
	cmd.Flags().StringVar(&hubURLFlag, "hub-url", "", "Arch-hub git URL (overrides config)")
	cmd.Flags().BoolVar(&noWaitFlag, "no-wait", false, "Submit and return ask-id without waiting (API mode only)")

	return cmd
}

// runLocalArchAsk queries the local askbox HTTP server, falling back to docker run for one-shot.
func runLocalArchAsk(question, hubURL, repos, adapter, model string) error {
	askboxURL := os.Getenv("ASKBOX_URL")
	if askboxURL == "" {
		cfg, _ := config.Load()
		if cfg != nil && cfg.AskboxURL != "" {
			askboxURL = cfg.AskboxURL
		} else {
			askboxURL = "http://localhost:8082"
		}
	}

	// Try the local askbox HTTP server first
	if err := tryAskboxServer(askboxURL, question, repos, adapter, model); err == nil {
		return nil
	}

	// Fallback: one-shot docker run (for users without the persistent server)
	return runDockerAsk(question, hubURL, repos, adapter, model)
}

// tryAskboxServer hits the local askbox HTTP API, returns nil on success.
func tryAskboxServer(baseURL, question, repos, adapter, model string) error {
	// Quick health check
	healthResp, err := httpGet(baseURL + "/health")
	if err != nil {
		return err // Server not running
	}
	_ = healthResp

	// Submit question
	body := map[string]any{"question": question}
	if repos != "" {
		body["repos"] = strings.Split(repos, ",")
	}
	if adapter != "" {
		body["adapter"] = adapter
	}
	if model != "" {
		body["model"] = model
	}

	var submitResp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := httpPost(baseURL+"/ask", body, &submitResp); err != nil {
		return err
	}

	jobID := submitResp.ID

	if !flagJSON && !flagAgent {
		fmt.Printf("  %s Submitted — job-id: %s\n", output.Green("✓"), jobID)
	}

	// Poll for completion
	for {
		var job struct {
			ID          string  `json:"id"`
			Status      string  `json:"status"`
			Answer      *string `json:"answer"`
			Error       *string `json:"error"`
			ToolCalls   int     `json:"tool_calls"`
			CompletedAt *float64 `json:"completed_at"`
			StartedAt   *float64 `json:"started_at"`
		}
		if err := httpGetJSON(baseURL+"/ask/"+jobID, &job); err != nil {
			return err
		}

		switch job.Status {
		case "completed":
			answer := ""
			if job.Answer != nil {
				answer = *job.Answer
			}
			elapsed := ""
			if job.StartedAt != nil && job.CompletedAt != nil {
				elapsed = fmt.Sprintf(", %.1fs", *job.CompletedAt-*job.StartedAt)
			}

			if flagJSON {
				return output.JSON(map[string]any{
					"success":    true,
					"jobId":      jobID,
					"answer":     answer,
					"chars":      len(answer),
					"toolCalls":  job.ToolCalls,
					"mode":       "askbox-server",
				})
			}
			if flagAgent {
				fmt.Print(answer)
				return nil
			}
			fmt.Printf("\r\033[K  %s Answer ready (%d chars, %d tool calls%s)\n\n",
				output.Green("✓"), len(answer), job.ToolCalls, elapsed)
			fmt.Println(answer)
			return nil

		case "failed":
			errMsg := "unknown error"
			if job.Error != nil {
				errMsg = *job.Error
			}
			if flagJSON {
				return output.JSON(map[string]any{
					"success": false,
					"jobId":   jobID,
					"error":   errMsg,
				})
			}
			return fmt.Errorf("askbox failed: %s", errMsg)

		default:
			if !flagJSON && !flagAgent {
				fmt.Printf("\r\033[K  %s %s (tool calls: %d)", output.Dim("⠋"), job.Status, job.ToolCalls)
			}
			time.Sleep(3 * time.Second)
		}
	}
}

// runDockerAsk runs the askbox as a one-shot Docker container (legacy/fallback).
func runDockerAsk(question, hubURL, repos, adapter, model string) error {
	// Resolve arch-hub URL: flag > config > error
	if hubURL == "" {
		cfg, _ := config.Load()
		if cfg != nil {
			hubURL = cfg.ArchHubURL
		}
	}
	if hubURL == "" {
		if flagJSON {
			return output.JSON(map[string]any{
				"success": false,
				"error":   "arch-hub URL required",
				"hint":    "use --hub-url or: reposwarm config set archHubUrl <git-url>",
			})
		}
		return fmt.Errorf("arch-hub URL required\n  💡 Use --hub-url <git-url> or: reposwarm config set archHubUrl <url>")
	}

	// Check Docker is available
	if _, err := exec.LookPath("docker"); err != nil {
		if flagJSON {
			return output.JSON(map[string]any{"success": false, "error": "docker not found in PATH"})
		}
		return fmt.Errorf("docker not found — install Docker to use --local")
	}

	if !flagJSON && !flagAgent {
		fmt.Printf("  %s Running askbox locally via Docker...\n", output.Dim("🐳"))
	}

	// Build docker run args
	args := []string{
		"run", "--rm",
		"-e", fmt.Sprintf("QUESTION=%s", question),
		"-e", fmt.Sprintf("ARCH_HUB_URL=%s", hubURL),
	}

	if repos != "" {
		args = append(args, "-e", fmt.Sprintf("REPOS_FILTER=%s", repos))
	}
	if adapter != "" {
		args = append(args, "-e", fmt.Sprintf("ASKBOX_ADAPTER=%s", adapter))
	}
	if model != "" {
		args = append(args, "-e", fmt.Sprintf("MODEL_ID=%s", model))
	}

	// Pass through LLM auth env vars (whatever the user has set)
	for _, env := range []string{
		"ANTHROPIC_API_KEY",
		"CLAUDE_CODE_USE_BEDROCK",
		"AWS_REGION",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"AWS_PROFILE",
		"AWS_BEARER_TOKEN_BEDROCK",
		"ANTHROPIC_BASE_URL",
		"LITELLM_API_URL",
		"LITELLM_API_KEY",
	} {
		if v := os.Getenv(env); v != "" {
			args = append(args, "-e", fmt.Sprintf("%s=%s", env, v))
		}
	}

	args = append(args, "ghcr.io/reposwarm/askbox:latest")

	cmd := exec.Command("docker", args...)
	cmd.Stderr = os.Stderr

	// Stream stdout for real-time output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		if flagJSON {
			return output.JSON(map[string]any{"success": false, "error": fmt.Sprintf("docker run failed: %v", err)})
		}
		return fmt.Errorf("docker run failed: %w", err)
	}

	// Collect output
	var answer strings.Builder
	var lastLine string
	inAnswer := false
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		// The askbox prints [askbox] status lines, then the answer after "ANSWER:"
		if strings.HasPrefix(line, "[askbox]") {
			if !flagJSON && !flagAgent {
				fmt.Printf("\r\033[K  %s %s\n", output.Dim("⠋"), strings.TrimPrefix(line, "[askbox] "))
			}
			continue
		}
		if strings.Contains(line, "ANSWER:") {
			inAnswer = true
			continue
		}
		if strings.Contains(line, "========") {
			continue
		}
		if inAnswer {
			answer.WriteString(line)
			answer.WriteString("\n")
		}
		lastLine = line
	}

	if err := cmd.Wait(); err != nil {
		if flagJSON {
			return output.JSON(map[string]any{"success": false, "error": fmt.Sprintf("askbox failed: %v", err)})
		}
		_ = lastLine
		return fmt.Errorf("askbox failed: %w", err)
	}

	result := strings.TrimSpace(answer.String())
	if result == "" {
		if flagJSON {
			return output.JSON(map[string]any{"success": false, "error": "askbox returned empty answer"})
		}
		return fmt.Errorf("askbox returned empty answer")
	}

	if flagJSON {
		return output.JSON(map[string]any{
			"success": true,
			"answer":  result,
			"chars":   len(result),
			"mode":    "local",
		})
	}

	if flagAgent {
		fmt.Print(result)
		return nil
	}

	fmt.Printf("\r\033[K  %s Answer ready (%d chars)\n\n", output.Green("✓"), len(result))
	fmt.Println(result)

	return nil
}

func runSimpleAsk(client *api.Client, question string) error {
	if !flagJSON && !flagAgent {
		fmt.Printf("  %s Thinking...\r", output.Dim("⏳"))
	}

	var resp struct {
		Success bool   `json:"success"`
		Answer  string `json:"answer"`
		Model   string `json:"model"`
		Latency int    `json:"latencyMs"`
		Error   string `json:"error"`
		Hint    string `json:"hint"`
	}

	err := client.Post(ctx(), "/ask", map[string]string{"question": question}, &resp)
	if err != nil {
		if flagJSON {
			return output.JSON(map[string]any{"success": false, "error": err.Error()})
		}
		return fmt.Errorf("ask failed: %w", err)
	}

	if !resp.Success {
		if flagJSON {
			return output.JSON(map[string]any{
				"success": false,
				"error":   resp.Error,
				"hint":    resp.Hint,
			})
		}
		msg := resp.Error
		if resp.Hint != "" {
			msg += "\n  💡 " + resp.Hint
		}
		return fmt.Errorf("%s", msg)
	}

	if flagJSON {
		return output.JSON(map[string]any{
			"success":   true,
			"answer":    resp.Answer,
			"model":     resp.Model,
			"latencyMs": resp.Latency,
		})
	}

	if flagAgent {
		fmt.Print(resp.Answer)
		return nil
	}

	// Clear the "Thinking..." line
	fmt.Print("\r\033[K")

	// Print answer with light formatting
	fmt.Println(resp.Answer)
	fmt.Println()
	fmt.Printf("  %s\n", output.Dim(fmt.Sprintf("— %s (%dms)", resp.Model, resp.Latency)))

	return nil
}

func runArchAsk(client *api.Client, question, repos, adapter string, noWait bool) error {

	body := map[string]string{"question": question}
	if repos != "" {
		body["repos"] = repos
	}
	if adapter != "" {
		body["adapter"] = adapter
	}

	// Submit the question
	var submitResp struct {
		Success bool   `json:"success"`
		AskID   string `json:"askId"`
		Status  string `json:"status"`
		Error   string `json:"error"`
	}

	if !flagJSON && !flagAgent {
		fmt.Printf("  %s Submitting question to askbox...\r", output.Dim("⏳"))
	}

	err := client.Post(ctx(), "/ask/arch", body, &submitResp)
	if err != nil {
		if flagJSON {
			return output.JSON(map[string]any{"success": false, "error": err.Error()})
		}
		return fmt.Errorf("ask failed: %w", err)
	}

	if !submitResp.Success {
		if flagJSON {
			return output.JSON(map[string]any{"success": false, "error": submitResp.Error})
		}
		return fmt.Errorf("ask failed: %s", submitResp.Error)
	}

	askID := submitResp.AskID

	// --no-wait: return immediately with the ask-id
	if noWait {
		if flagJSON {
			return output.JSON(map[string]any{
				"success": true,
				"askId":   askID,
				"status":  "pending",
			})
		}
		if flagAgent {
			fmt.Printf("ask-id: %s\nstatus: pending\n", askID)
			return nil
		}
		fmt.Printf("  %s Submitted — ask-id: %s (use reposwarm ask status %s to check)\n",
			output.Green("✓"), askID, askID)
		return nil
	}

	if !flagJSON && !flagAgent {
		fmt.Printf("\r\033[K  %s Submitted — ask-id: %s\n", output.Green("✓"), askID)
	}
	if flagAgent {
		fmt.Fprintf(os.Stderr, "ask-id: %s\nstatus: polling\n", askID)
	}

	// Poll for completion
	for {
		var pollResp struct {
			Success     bool   `json:"success"`
			AskID       string `json:"askId"`
			Status      string `json:"status"`
			Detail      string `json:"detail"`
			Answer      string `json:"answer"`
			DownloadURL string `json:"downloadUrl"`
			Error       string `json:"error"`
			Chars       int    `json:"chars"`
		}

		err := client.Get(ctx(), fmt.Sprintf("/ask/arch/%s", askID), &pollResp)
		if err != nil {
			if flagJSON {
				return output.JSON(map[string]any{
					"success": false,
					"askId":   askID,
					"error":   err.Error(),
				})
			}
			return fmt.Errorf("polling failed: %w", err)
		}

		switch pollResp.Status {
		case "completed":
			if flagJSON {
				return output.JSON(map[string]any{
					"success": true,
					"askId":   askID,
					"answer":  pollResp.Answer,
					"chars":   pollResp.Chars,
				})
			}
			if flagAgent {
				fmt.Print(pollResp.Answer)
				return nil
			}
			fmt.Printf("\r\033[K  %s Answer ready (%d chars)\n\n", output.Green("✓"), pollResp.Chars)
			fmt.Println(pollResp.Answer)
			return nil

		case "failed":
			if flagJSON {
				return output.JSON(map[string]any{
					"success": false,
					"askId":   askID,
					"status":  "failed",
					"error":   pollResp.Error,
				})
			}
			return fmt.Errorf("ask failed: %s", pollResp.Error)

		default:
			if !flagJSON && !flagAgent {
				detail := pollResp.Detail
				if detail == "" {
					detail = pollResp.Status
				}
				fmt.Printf("\r\033[K  %s %s", output.Dim("⠋"), detail)
			}
			time.Sleep(3 * time.Second)
		}
	}
}

// ---------------------------------------------------------------------------
// HTTP helpers for direct askbox communication
// ---------------------------------------------------------------------------

func httpGet(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func httpGetJSON(url string, target any) error {
	body, err := httpGet(url)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}

func httpPost(url string, payload any, target any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, target)
}
