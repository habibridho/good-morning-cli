package config

import (
	"os"
	"testing"

	"github.com/habib-ridho/good-morning/internal/tracker"
)

func TestParseMembers(t *testing.T) {
	raw := "alice_jira|alice_lark|Alice|tester,bob_jira:bob_lark:Bob,712020:uuid|lark_id|Charlie|developer"
	members, err := parseMembers(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(members))
	}

	if members[0].Name != "Alice" || members[0].TrackerUserID != "alice_jira" || members[0].MessengerUserID != "alice_lark" || members[0].Role != "tester" {
		t.Errorf("unexpected parsed member 0: %+v", members[0])
	}
	// Bob uses fallback ':'
	if members[1].Name != "Bob" || members[1].TrackerUserID != "bob_jira" || members[1].MessengerUserID != "bob_lark" || members[1].Role != "developer" {
		t.Errorf("unexpected parsed member 1: %+v", members[1])
	}
	// Charlie uses '|' and has colon in Jira ID
	if members[2].Name != "Charlie" || members[2].TrackerUserID != "712020:uuid" || members[2].MessengerUserID != "lark_id" || members[2].Role != "developer" {
		t.Errorf("unexpected parsed member 2: %+v", members[2])
	}
}

func TestParseMembers_InvalidFormat(t *testing.T) {
	raw := "jira1:lark1" // missing name field
	_, err := parseMembers(raw)
	if err == nil {
		t.Error("expected error for invalid member format (missing name), got nil")
	}
}

func TestParseMembers_Empty(t *testing.T) {
	_, err := parseMembers("")
	if err == nil {
		t.Error("expected error for empty member string, got nil")
	}
}

func TestParseLaneMapping(t *testing.T) {
	raw := `{"Dev Done": "Ready to Deploy", "Coding": "In Progress"}`
	mapping, err := parseLaneMapping(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mapping) != 2 {
		t.Fatalf("expected 2 mapped lanes, got %d", len(mapping))
	}

	if mapping["Dev Done"] != tracker.LaneGroupReadyToDeploy {
		t.Errorf("expected Dev Done to map to Ready to Deploy, got %v", mapping["Dev Done"])
	}

	if mapping["Coding"] != tracker.LaneGroupInProgress {
		t.Errorf("expected Coding to map to In Progress, got %v", mapping["Coding"])
	}
}

func TestParseLaneMapping_InvalidGroup(t *testing.T) {
	raw := `{"Dev Done": "Invalid Group"}`
	_, err := parseLaneMapping(raw)
	if err == nil {
		t.Error("expected error for invalid lane group, got nil")
	}
}

func TestParseLaneMapping_InvalidJSON(t *testing.T) {
	_, err := parseLaneMapping(`not json`)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestLoad_BasicFields(t *testing.T) {
	t.Setenv("GOOD_MORNING_JIRA_BASE_URL", "https://jira.example.com")
	t.Setenv("GOOD_MORNING_JIRA_BOARD_ID", "123")
	t.Setenv("GOOD_MORNING_TEAM_MEMBERS", "a:b:Alice")
	t.Setenv("GOOD_MORNING_JIRA_TOKEN", "")
	t.Setenv("GOOD_MORNING_LLM_BASE_URL", "")
	t.Setenv("GOOD_MORNING_LANE_MAPPING", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.JiraBaseURL != "https://jira.example.com" {
		t.Errorf("expected url https://jira.example.com, got %v", cfg.JiraBaseURL)
	}
	if cfg.JiraBoardID != 123 {
		t.Errorf("expected board id 123, got %v", cfg.JiraBoardID)
	}
	if len(cfg.Members) != 1 {
		t.Errorf("expected 1 member, got %v", len(cfg.Members))
	}
}

func TestLoad_InvalidBoardID(t *testing.T) {
	t.Setenv("GOOD_MORNING_JIRA_BOARD_ID", "notanumber")
	_, err := Load()
	if err == nil {
		t.Error("expected error for non-numeric board ID")
	}
}

func TestLoad_InvalidLaneMappingGroup(t *testing.T) {
	t.Setenv("GOOD_MORNING_JIRA_BOARD_ID", "1")
	t.Setenv("GOOD_MORNING_LANE_MAPPING", `{"In Review":"BadGroup"}`)
	_, err := Load()
	if err == nil {
		t.Error("expected error for invalid lane group name")
	}
}

func TestValidateRun_MissingRequired(t *testing.T) {
	// Unset all relevant vars
	keys := []string{
		"GOOD_MORNING_JIRA_BASE_URL", "GOOD_MORNING_JIRA_EMAIL", "GOOD_MORNING_JIRA_TOKEN", "GOOD_MORNING_JIRA_BOARD_ID",
		"GOOD_MORNING_LLM_BASE_URL", "GOOD_MORNING_LARK_APP_ID", "GOOD_MORNING_LARK_APP_SECRET",
		"GOOD_MORNING_LARK_GROUP_CHAT_ID", "GOOD_MORNING_TEAM_MEMBERS", "GOOD_MORNING_LANE_MAPPING",
	}
	for _, k := range keys {
		os.Unsetenv(k)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load should not error with empty config: %v", err)
	}

	err = cfg.ValidateRun()
	if err == nil {
		t.Fatal("expected validation error for empty config")
	}

	if _, ok := err.(*MissingConfigError); !ok {
		t.Errorf("expected *MissingConfigError, got %T: %v", err, err)
	}
}
