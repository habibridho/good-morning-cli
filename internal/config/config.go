package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/habib-ridho/good-morning/internal/messenger"
	"github.com/habib-ridho/good-morning/internal/planner"
	"github.com/habib-ridho/good-morning/internal/tracker"
)

const envPrefix = "GOOD_MORNING_"

// Config holds all runtime configuration for good-morning.
type Config struct {
	// Jira
	JiraBaseURL string
	JiraEmail   string
	JiraToken   string
	JiraBoardID int

	// LLM
	LLMBaseURL     string
	LLMModel       string
	LLMSystemPrompt string
	LLMTimeoutSec  int

	// Lark
	LarkAppID      string
	LarkAppSecret  string
	LarkGroupChatID string

	// Team
	Members     []messenger.Member
	LaneMapping planner.LaneMapping
}

// MissingConfigError is returned when a required env var is not set.
type MissingConfigError struct {
	Keys []string
}

func (e *MissingConfigError) Error() string {
	return fmt.Sprintf("missing required configuration: %s", strings.Join(e.Keys, ", "))
}

// Load reads all configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		JiraBaseURL:     env("JIRA_BASE_URL"),
		JiraEmail:       env("JIRA_EMAIL"),
		JiraToken:       env("JIRA_TOKEN"),
		LLMBaseURL:      env("LLM_BASE_URL"),
		LLMModel:        env("LLM_MODEL"),
		LLMSystemPrompt: env("LLM_SYSTEM_PROMPT"),
		LarkAppID:       env("LARK_APP_ID"),
		LarkAppSecret:   env("LARK_APP_SECRET"),
		LarkGroupChatID: env("LARK_GROUP_CHAT_ID"),
	}

	// Numeric fields
	if v := env("JIRA_BOARD_ID"); v != "" {
		id, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("GOOD_MORNING_JIRA_BOARD_ID must be a number: %w", err)
		}
		cfg.JiraBoardID = id
	}

	if v := env("LLM_TIMEOUT_SEC"); v != "" {
		t, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("GOOD_MORNING_LLM_TIMEOUT_SEC must be a number: %w", err)
		}
		cfg.LLMTimeoutSec = t
	}

	// Team members: "jira_id:lark_id:name,jira_id:lark_id:name,..."
	if v := env("TEAM_MEMBERS"); v != "" {
		members, err := parseMembers(v)
		if err != nil {
			return nil, fmt.Errorf("parsing GOOD_MORNING_TEAM_MEMBERS: %w", err)
		}
		cfg.Members = members
	}

	// Lane mapping: JSON {"Jira Column": "Framework Group", ...}
	if v := env("LANE_MAPPING"); v != "" {
		mapping, err := parseLaneMapping(v)
		if err != nil {
			return nil, fmt.Errorf("parsing GOOD_MORNING_LANE_MAPPING: %w", err)
		}
		cfg.LaneMapping = mapping
	}

	return cfg, nil
}

// ValidateRun validates that all required fields for the run command are set.
func (c *Config) ValidateRun() error {
	var missing []string
	if c.JiraBaseURL == "" {
		missing = append(missing, "GOOD_MORNING_JIRA_BASE_URL")
	}
	if c.JiraEmail == "" {
		missing = append(missing, "GOOD_MORNING_JIRA_EMAIL")
	}
	if c.JiraToken == "" {
		missing = append(missing, "GOOD_MORNING_JIRA_TOKEN")
	}
	if c.JiraBoardID == 0 {
		missing = append(missing, "GOOD_MORNING_JIRA_BOARD_ID")
	}
	if c.LLMBaseURL == "" {
		missing = append(missing, "GOOD_MORNING_LLM_BASE_URL")
	}
	if c.LarkAppID == "" {
		missing = append(missing, "GOOD_MORNING_LARK_APP_ID")
	}
	if c.LarkAppSecret == "" {
		missing = append(missing, "GOOD_MORNING_LARK_APP_SECRET")
	}
	if c.LarkGroupChatID == "" {
		missing = append(missing, "GOOD_MORNING_LARK_GROUP_CHAT_ID")
	}
	if len(c.Members) == 0 {
		missing = append(missing, "GOOD_MORNING_TEAM_MEMBERS")
	}
	if len(c.LaneMapping) == 0 {
		missing = append(missing, "GOOD_MORNING_LANE_MAPPING")
	}

	if len(missing) > 0 {
		return &MissingConfigError{Keys: missing}
	}
	return nil
}

// env reads an env var with the GOOD_MORNING_ prefix.
func env(key string) string {
	return os.Getenv(envPrefix + key)
}

// parseMembers parses "jira_id:lark_id:name:role,..." into []messenger.Member.
func parseMembers(raw string) ([]messenger.Member, error) {
	parts := strings.Split(raw, ",")
	members := make([]messenger.Member, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var fields []string
		if strings.Contains(part, "|") {
			fields = strings.Split(part, "|")
		} else {
			fields = strings.Split(part, ":")
		}
		
		if len(fields) < 3 {
			return nil, fmt.Errorf("invalid member entry %q: expected jira_id|lark_id|name[|role]", part)
		}
		
		// If using ':', and length is > 4, it's likely a jira_id with a colon that failed parsing.
		// Tell them to use '|' instead.
		if !strings.Contains(part, "|") && len(fields) > 4 {
			return nil, fmt.Errorf("invalid member entry %q: too many colons, please use '|' as delimiter instead", part)
		}

		// Parse from right to left to allow jira_id to contain colons if they still use ':' and len is 4 or 3
		var jiraID, larkID, name, role string
		role = "developer"
		
		if len(fields) == 4 {
			role = strings.TrimSpace(fields[3])
			name = strings.TrimSpace(fields[2])
			larkID = strings.TrimSpace(fields[1])
			jiraID = strings.TrimSpace(fields[0])
		} else if len(fields) == 3 {
			name = strings.TrimSpace(fields[2])
			larkID = strings.TrimSpace(fields[1])
			jiraID = strings.TrimSpace(fields[0])
		} else if len(fields) > 4 && strings.Contains(part, "|") {
			// If using '|' and there's somehow more than 4, we just take the first 4
			role = strings.TrimSpace(fields[3])
			name = strings.TrimSpace(fields[2])
			larkID = strings.TrimSpace(fields[1])
			jiraID = strings.TrimSpace(fields[0])
		} else {
			// for the case of len(fields) > 4 and no '|', which we already handled above
			return nil, fmt.Errorf("invalid member entry %q", part)
		}

		members = append(members, messenger.Member{
			TrackerUserID:   jiraID,
			MessengerUserID: larkID,
			Name:            name,
			Role:            role,
		})
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("no valid members found")
	}
	return members, nil
}

// parseLaneMapping parses a JSON lane mapping into a planner.LaneMapping.
func parseLaneMapping(raw string) (planner.LaneMapping, error) {
	var rawMap map[string]string
	if err := json.Unmarshal([]byte(raw), &rawMap); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	mapping := make(planner.LaneMapping, len(rawMap))
	for column, groupStr := range rawMap {
		group := tracker.LaneGroup(groupStr)
		// validate group
		valid := false
		for _, g := range tracker.LaneGroupOrder {
			if g == group {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("unknown lane group %q for column %q", groupStr, column)
		}
		mapping[column] = group
	}

	return mapping, nil
}
