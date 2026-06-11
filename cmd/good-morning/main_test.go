package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestEndToEndRun(t *testing.T) {
	// 1. Setup Mock Jira Server
	jiraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/sprint") && strings.Contains(r.URL.RawQuery, "state=active") {
			// Mock Active Sprint
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"values": [{"id": 1, "name": "Sprint 1", "state": "active"}]}`))
			return
		}

		if strings.Contains(r.URL.Path, "/issue") {
			// Mock Sprint Issues
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"issues": [
					{
						"key": "A-1",
						"fields": {
							"summary": "Fix login bug",
							"status": {"name": "In Progress"},
							"priority": {"name": "High"},
							"assignee": {"accountId": "alice_jira", "displayName": "Alice"}
						}
					}
				],
				"total": 1
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer jiraServer.Close()

	// 2. Setup Mock LLM Server
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/chat/completions") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"choices": [
					{
						"message": {"role": "assistant", "content": "Good morning team! @lark_alice is working on fixing the login bug. Let's have a great day!"}
					}
				]
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer llmServer.Close()

	// 3. Setup Mock Lark Server
	// var larkMessageSent string (not used in dry-run test)
	larkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/v3/tenant_access_token/internal") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code": 0, "msg": "ok", "tenant_access_token": "mock-token"}`))
			return
		}

		if strings.Contains(r.URL.Path, "/im/v1/messages") {
			// In dry-run mode, we don't expect this to be called, but we stub it anyway.
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code": 0, "msg": "ok"}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer larkServer.Close()

	// 4. Set Environment Variables for Config
	os.Setenv("GOOD_MORNING_JIRA_BASE_URL", jiraServer.URL)
	os.Setenv("GOOD_MORNING_JIRA_EMAIL", "test@example.com")
	os.Setenv("GOOD_MORNING_JIRA_TOKEN", "mock-jira-token")
	os.Setenv("GOOD_MORNING_JIRA_BOARD_ID", "123")
	
	os.Setenv("GOOD_MORNING_LLM_BASE_URL", llmServer.URL)
	os.Setenv("GOOD_MORNING_LLM_MODEL", "mock-model")

	// Set Lark base URL using a hack or just don't set it if the internal client doesn't support changing base URL via env
	// Wait, Lark messenger currently hardcodes defaultBaseURL unless NewClientWithBaseURL is used. 
	// To test Lark via E2E without modifying main.go extensively, we could use dry-run, but we want to test E2E.
	// For this test, let's just use dry-run to test the flow up to LLM message generation,
	// because Lark client in main.go uses lark.NewClient which has a hardcoded base URL.
	// We'll verify dry-run works and output is printed.
	
	os.Setenv("GOOD_MORNING_LARK_APP_ID", "mock-app")
	os.Setenv("GOOD_MORNING_LARK_APP_SECRET", "mock-secret")
	os.Setenv("GOOD_MORNING_LARK_GROUP_CHAT_ID", "mock-chat")
	
	os.Setenv("GOOD_MORNING_TEAM_MEMBERS", "alice_jira:lark_alice:Alice")
	os.Setenv("GOOD_MORNING_LANE_MAPPING", `{"In Progress": "In Progress"}`)

	defer os.Clearenv()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Execute Run command in dry-run mode to avoid hitting actual Lark API
	rootCmd.SetArgs([]string{"run", "--dry-run"})
	err := rootCmd.Execute()
	
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error running command: %v", err)
	}

	buf := new(strings.Builder)
	_, _ = io.Copy(buf, r)
	output := buf.String()

	if !strings.Contains(output, "DRY RUN MESSAGE") {
		t.Errorf("expected output to contain DRY RUN MESSAGE, got:\n%s", output)
	}
	if !strings.Contains(output, "Good morning team! @lark_alice is working on fixing the login bug.") {
		t.Errorf("expected output to contain LLM generated text, got:\n%s", output)
	}
}
