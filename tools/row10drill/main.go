package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const prompt = `You are the agent "drill" working in the Tickets issue tracker through its MCP server.
Perform this workflow using only the tickets MCP tools, without running shell commands:
find the work assigned to you, read its linked context, start the ticket, leave a
comment recording what you found, record a decision, then complete the ticket.`

type runResult struct {
	Host         string        `json:"host"`
	Run          int           `json:"run"`
	Project      string        `json:"project"`
	Passed       bool          `json:"passed"`
	Failures     []string      `json:"failures,omitempty"`
	FirstCall    string        `json:"first_call"`
	CallCount    int           `json:"call_count"`
	ToolErrors   int           `json:"tool_errors"`
	SchemaErrors int           `json:"schema_errors"`
	Sequence     []string      `json:"sequence"`
	State        workflowState `json:"state"`
	Duration     string        `json:"duration"`
	Transcript   string        `json:"transcript"`
	SetupFailed  bool          `json:"setup_failed,omitempty"`
}

func main() {
	bin := flag.String("bin", filepath.Join("bin", "tickets"), "path to the built tickets binary")
	hosts := flag.String("hosts", "claude,codex", "comma-separated agent hosts to drill")
	runs := flag.Int("n", 1, "runs per host")
	artifacts := flag.String("artifacts", filepath.Join("artifacts", "row10"), "directory for transcripts and server logs")
	timeout := flag.Duration("timeout", 10*time.Minute, "per-run timeout for the agent host")
	flag.Parse()

	binPath, err := filepath.Abs(*bin)
	if err != nil {
		fail(err)
	}
	if _, err := os.Stat(binPath); err != nil {
		fail(fmt.Errorf("tickets binary not found at %s (run `task build` first): %w", binPath, err))
	}
	artifactDir, err := filepath.Abs(*artifacts)
	if err != nil {
		fail(err)
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		fail(err)
	}

	var results []runResult
	for _, host := range strings.Split(*hosts, ",") {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if _, err := exec.LookPath(host); err != nil {
			fail(fmt.Errorf("agent host %q is not on PATH: %w", host, err))
		}
		for i := 1; i <= *runs; i++ {
			results = append(results, drill(binPath, host, i, artifactDir, *timeout))
		}
	}

	report(results, artifactDir)
}

func drill(bin, host string, run int, artifactDir string, timeout time.Duration) runResult {
	project := fmt.Sprintf("D%s%d%02d", strings.ToUpper(host[:1]), run, rand.Intn(100))
	res := runResult{Host: host, Run: run, Project: project}
	started := time.Now()

	fmt.Fprintf(os.Stderr, "=== %s run %d (project %s)\n", host, run, project)

	f, err := newFixture(bin, artifactDir, project)
	defer f.close()
	if err != nil {
		res.SetupFailed = true
		res.Failures = []string{"fixture setup failed: " + err.Error()}
		res.Duration = time.Since(started).String()
		return res
	}

	transcriptPath := filepath.Join(artifactDir, fmt.Sprintf("%s-run%d-%s.jsonl", host, run, project))
	res.Transcript = transcriptPath

	tr, err := runHost(host, f, transcriptPath, timeout)
	if err != nil {
		res.Failures = append(res.Failures, "agent host failed: "+err.Error())
	}
	if tr.HostError != "" {
		res.Failures = append(res.Failures, tr.HostError)
	}

	res.Sequence = tr.sequence()
	res.CallCount = len(tr.Calls)
	res.ToolErrors = tr.errorCount()
	res.FirstCall = tr.firstCall()

	res.State = f.inspect()
	res.Failures = append(res.Failures, res.State.failures()...)
	for _, c := range tr.schemaErrors() {
		res.SchemaErrors++
		res.Failures = append(res.Failures, fmt.Sprintf(
			"%s was called with arguments the tool surface rejected: %s", c.Tool, c.Error))
	}
	res.Passed = len(res.Failures) == 0
	res.Duration = time.Since(started).Round(time.Second).String()
	return res
}

func runHost(host string, f *fixture, transcriptPath string, timeout time.Duration) (transcript, error) {
	out, err := os.Create(transcriptPath)
	if err != nil {
		return transcript{}, err
	}
	defer func() { _ = out.Close() }()

	errPath := strings.TrimSuffix(transcriptPath, ".jsonl") + ".stderr.log"
	errFile, err := os.Create(errPath)
	if err != nil {
		return transcript{}, err
	}
	defer func() { _ = errFile.Close() }()

	var cmd *exec.Cmd
	switch host {
	case "claude":
		cmd, err = claudeCommand(f, filepath.Dir(transcriptPath))
	case "codex":
		cmd = codexCommand(f)
	default:
		return transcript{}, fmt.Errorf("unknown agent host %q (want claude or codex)", host)
	}
	if err != nil {
		return transcript{}, err
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return transcript{}, err
	}
	defer func() { _ = devNull.Close() }()

	cmd.Stdin = devNull
	cmd.Stdout = out
	cmd.Stderr = errFile

	runErr := runWithTimeout(cmd, timeout)

	if _, err := out.Seek(0, 0); err != nil {
		return transcript{}, err
	}
	var tr transcript
	if host == "claude" {
		tr, err = parseClaudeTranscript(out)
	} else {
		tr, err = parseCodexTranscript(out)
	}
	if err != nil {
		return tr, err
	}
	return tr, runErr
}

func runWithTimeout(cmd *exec.Cmd, timeout time.Duration) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return fmt.Errorf("timed out after %s", timeout)
	}
}

func claudeCommand(f *fixture, dir string) (*exec.Cmd, error) {
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"tickets": map[string]any{
				"command": f.bin,
				"args":    []string{"mcp", "--url", f.baseURL, "--token", f.token},
			},
		},
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(dir, fmt.Sprintf("mcp-%s.json", f.project))
	if err := os.WriteFile(cfgPath, body, 0o600); err != nil {
		return nil, err
	}

	return exec.Command("claude", "-p", prompt,
		"--mcp-config", cfgPath,
		"--strict-mcp-config",
		"--allowed-tools", "mcp__tickets",
		"--output-format", "stream-json",
		"--verbose",
	), nil
}

func codexCommand(f *fixture) *exec.Cmd {
	args, err := json.Marshal([]string{"mcp", "--url", f.baseURL, "--token", f.token})
	if err != nil {
		args = []byte("[]")
	}
	return exec.Command("codex", "exec",
		"--json",
		"--ignore-user-config",
		"--skip-git-repo-check",
		"--ephemeral",
		"--approve-for-me",
		"-c", fmt.Sprintf("mcp_servers.tickets.command=%q", f.bin),
		"-c", "mcp_servers.tickets.args="+string(args),
		prompt,
	)
}

func report(results []runResult, artifactDir string) {
	summaryPath := filepath.Join(artifactDir, "summary.json")
	body, err := json.MarshalIndent(results, "", "  ")
	if err == nil {
		_ = os.WriteFile(summaryPath, body, 0o644)
	}

	perHost := map[string][2]int{}
	fmt.Println()
	fmt.Println("Row 10 live-agent drill")
	fmt.Println(strings.Repeat("-", 72))
	for _, r := range results {
		status := "FAIL"
		if r.Passed {
			status = "PASS"
		}
		counts := perHost[r.Host]
		counts[1]++
		if r.Passed {
			counts[0]++
		}
		perHost[r.Host] = counts

		fmt.Printf("%-6s run %d  %s  %s  first=%s calls=%d tool_errors=%d schema_errors=%d\n",
			r.Host, r.Run, status, r.Duration, orNone(r.FirstCall), r.CallCount, r.ToolErrors, r.SchemaErrors)
		if len(r.Sequence) > 0 {
			fmt.Printf("        sequence: %s\n", strings.Join(r.Sequence, " → "))
		}
		for _, f := range r.Failures {
			fmt.Printf("        ! %s\n", f)
		}
	}
	fmt.Println(strings.Repeat("-", 72))

	exitCode := 0
	for host, counts := range perHost {
		fmt.Printf("%-6s %d/%d passed\n", host, counts[0], counts[1])
		if counts[0] < counts[1] {
			exitCode = 1
		}
	}
	fmt.Printf("\nartifacts: %s\n", artifactDir)

	if exitCode != 0 {
		os.Exit(1)
	}
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "row10drill:", err)
	os.Exit(2)
}
