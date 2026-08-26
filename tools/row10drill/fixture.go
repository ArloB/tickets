package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type fixture struct {
	bin      string
	dataDir  string
	port     int
	baseURL  string
	token    string
	project  string
	ticket   string
	server   *exec.Cmd
	logFile  *os.File
	teardown []func()
}

const (
	adminUser   = "drilladmin"
	adminPass   = "Drill-passw0rd!"
	agentName   = "drill"
	ticketTitle = "Fix the flaky export checksum"
	ticketBody  = "Reruns of the export produce a different checksum for identical content. Investigate the cause and record the decision."
)

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func (f *fixture) run(args ...string) ([]byte, error) {
	cmd := exec.Command(f.bin, args...)
	cmd.Env = append(os.Environ(),
		"TICKETS_API_URL="+f.baseURL,
		"TICKETS_API_TOKEN="+f.token,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %v: %w: %s", filepath.Base(f.bin), args, err, out)
	}
	return out, nil
}

func newFixture(bin, artifactDir, projectKey string) (*fixture, error) {
	f := &fixture{bin: bin, project: projectKey}
	dataDir, err := os.MkdirTemp("", "row10drill-")
	if err != nil {
		return f, err
	}
	f.dataDir = dataDir
	f.teardown = append(f.teardown, func() { _ = os.RemoveAll(dataDir) })

	if f.port, err = freePort(); err != nil {
		return f, err
	}
	f.baseURL = fmt.Sprintf("http://127.0.0.1:%d/api/v1", f.port)

	if _, err := f.run("setup", "--data-dir", dataDir, "--username", adminUser, "--password", adminPass); err != nil {
		return f, err
	}
	if err := f.startServer(artifactDir); err != nil {
		return f, err
	}
	if _, err := f.run("admin", "agent", "create", "--data-dir", dataDir, "--name", agentName, "--as", adminUser); err != nil {
		return f, err
	}
	if err := f.issueToken(); err != nil {
		return f, err
	}
	if err := f.seed(); err != nil {
		return f, err
	}
	return f, nil
}

func (f *fixture) startServer(artifactDir string) error {
	logPath := filepath.Join(artifactDir, fmt.Sprintf("server-%s.log", f.project))
	logFile, err := os.Create(logPath)
	if err != nil {
		return err
	}
	f.logFile = logFile

	cmd := exec.Command(f.bin, "server", "--data-dir", f.dataDir, "--port", fmt.Sprint(f.port))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return err
	}
	f.server = cmd
	f.teardown = append(f.teardown, func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = logFile.Close()
	})

	health := fmt.Sprintf("http://127.0.0.1:%d/healthz", f.port)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(health)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("server did not become healthy at %s within 20s", health)
}

func (f *fixture) issueToken() error {
	out, err := f.run("admin", "token", "create", agentName, "--data-dir", f.dataDir, "--as", adminUser, "--json")
	if err != nil {
		return err
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(out, &created); err != nil {
		return fmt.Errorf("parse token JSON: %w", err)
	}
	if created.Token == "" {
		return fmt.Errorf("admin token create returned no token")
	}
	f.token = created.Token
	return nil
}

func (f *fixture) seed() error {
	if _, err := f.run("project", "create", "--key", f.project,
		"--title", "Row 10 Drill", "--description", "Fixture project for the row-10 live-agent drill."); err != nil {
		return err
	}
	out, err := f.run("ticket", "create", "--project", f.project, "--type", "task",
		"--title", ticketTitle, "--priority", "high", "--description", ticketBody, "--json")
	if err != nil {
		return err
	}
	var created struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(out, &created); err != nil {
		return fmt.Errorf("parse ticket JSON: %w", err)
	}
	f.ticket = created.Ref
	_, err = f.run("ticket", "assign", f.ticket, "--assignee", "agent:"+agentName, "--if-version", "1", "--json")
	return err
}

func (f *fixture) close() {
	for i := len(f.teardown) - 1; i >= 0; i-- {
		f.teardown[i]()
	}
}

type workflowState struct {
	Status        string `json:"status"`
	Assignee      string `json:"assignee"`
	AgentComments int    `json:"agent_comments"`
	Decisions     int    `json:"decisions"`
	StatusChanges int    `json:"agent_status_changes"`
	InspectionErr string `json:"inspection_error,omitempty"`
}

func (f *fixture) inspect() workflowState {
	var st workflowState

	out, err := f.run("ticket", "get", f.ticket, "--json")
	if err != nil {
		st.InspectionErr = err.Error()
		return st
	}
	var ticket struct {
		Status   string `json:"status"`
		Assignee string `json:"assignee"`
	}
	if err := json.Unmarshal(out, &ticket); err != nil {
		st.InspectionErr = err.Error()
		return st
	}
	st.Status, st.Assignee = ticket.Status, ticket.Assignee

	out, err = f.run("comment", "list", f.ticket, "--json")
	if err != nil {
		st.InspectionErr = err.Error()
		return st
	}
	var comments struct {
		Comments []struct {
			Author string `json:"author"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(out, &comments); err != nil {
		st.InspectionErr = err.Error()
		return st
	}
	for _, c := range comments.Comments {
		if c.Author == "agent:"+agentName {
			st.AgentComments++
		}
	}

	out, err = f.run("decision", "list", "--project", f.project, "--json")
	if err != nil {
		st.InspectionErr = err.Error()
		return st
	}
	var decisions struct {
		Decisions []struct {
			Ref string `json:"ref"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal(out, &decisions); err != nil {
		st.InspectionErr = err.Error()
		return st
	}
	st.Decisions = len(decisions.Decisions)

	out, err = f.run("activity", "list", "--project", f.project,
		"--event-type", "ticket_status_changed", "--limit", "100", "--json")
	if err != nil {
		st.InspectionErr = err.Error()
		return st
	}
	var activity struct {
		Events []struct {
			Entity string `json:"entity"`
			Actor  string `json:"actor"`
		} `json:"events"`
	}
	if err := json.Unmarshal(out, &activity); err != nil {
		st.InspectionErr = err.Error()
		return st
	}
	for _, e := range activity.Events {
		if e.Entity == f.ticket && e.Actor == "agent:"+agentName {
			st.StatusChanges++
		}
	}
	return st
}

func (st workflowState) failures() []string {
	var out []string
	if st.InspectionErr != "" {
		return []string{"could not inspect server state: " + st.InspectionErr}
	}
	if st.Status != "done" {
		out = append(out, fmt.Sprintf("ticket status is %q, want \"done\"", st.Status))
	}
	if st.StatusChanges < 2 {
		out = append(out, fmt.Sprintf("%d agent status change(s) on the ticket, want at least 2 "+
			"(starting it and completing it are separate workflow steps; one change means it jumped straight to done)",
			st.StatusChanges))
	}
	if st.Assignee != "agent:"+agentName {
		out = append(out, fmt.Sprintf("ticket assignee is %q, want %q", st.Assignee, "agent:"+agentName))
	}
	if st.AgentComments < 1 {
		out = append(out, "no comment attributed to agent:"+agentName)
	}
	if st.Decisions < 1 {
		out = append(out, "no decision recorded in the project")
	}
	return out
}
