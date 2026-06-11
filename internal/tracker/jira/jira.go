package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/habib-ridho/good-morning/internal/tracker"
)

// Client is a Jira Agile REST API client.
type Client struct {
	baseURL    string
	email      string
	token      string
	boardID    int
	httpClient *http.Client
}

// NewClient creates a new Jira client.
// email is the user's Jira email, token is a Jira API Token.
func NewClient(baseURL, email, token string, boardID int) *Client {
	return &Client{
		baseURL: baseURL,
		email:   email,
		token:   token,
		boardID: boardID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// --- API response types ---

type sprintListResponse struct {
	Values []sprintValue `json:"values"`
}

type sprintValue struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

type issueListResponse struct {
	Issues []jiraIssue `json:"issues"`
	Total  int         `json:"total"`
}

type jiraIssue struct {
	Key    string          `json:"key"`
	Fields jiraIssueFields `json:"fields"`
}

type jiraIssueFields struct {
	Summary  string        `json:"summary"`
	Status   jiraStatus    `json:"status"`
	Priority jiraPriority  `json:"priority"`
	Assignee *jiraAssignee `json:"assignee"`
}

type jiraStatus struct {
	Name string `json:"name"`
}

type jiraPriority struct {
	Name string `json:"name"`
}

type jiraAssignee struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
}

type boardConfigResponse struct {
	ColumnConfig struct {
		Columns []boardColumn `json:"columns"`
	} `json:"columnConfig"`
}

type boardColumn struct {
	Name     string `json:"name"`
	Statuses []struct {
		ID string `json:"id"`
	} `json:"statuses"`
}

// BoardMember represents a Jira user associated with the board.
type BoardMember struct {
	AccountID   string
	DisplayName string
}

// --- Interface implementation ---

// GetActiveSprint returns the currently active sprint for the board.
func (c *Client) GetActiveSprint(ctx context.Context) (*tracker.Sprint, error) {
	url := fmt.Sprintf("%s/rest/agile/1.0/board/%d/sprint?state=active", c.baseURL, c.boardID)

	req, err := c.newRequest(ctx, http.MethodGet, url)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching active sprint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira sprint list: unexpected status %d", resp.StatusCode)
	}

	var result sprintListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding sprint list: %w", err)
	}

	if len(result.Values) == 0 {
		return nil, fmt.Errorf("no active sprint found on board %d", c.boardID)
	}

	s := result.Values[0]
	return &tracker.Sprint{ID: s.ID, Name: s.Name}, nil
}

// GetSprintIssues returns all issues in the given sprint.
func (c *Client) GetSprintIssues(ctx context.Context, sprintID int) ([]tracker.Issue, error) {
	var issues []tracker.Issue
	startAt := 0
	maxResults := 100

	for {
		url := fmt.Sprintf(
			"%s/rest/agile/1.0/sprint/%d/issue?startAt=%d&maxResults=%d",
			c.baseURL, sprintID, startAt, maxResults,
		)

		req, err := c.newRequest(ctx, http.MethodGet, url)
		if err != nil {
			return nil, fmt.Errorf("building request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetching sprint issues: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("jira sprint issues: unexpected status %d", resp.StatusCode)
		}

		var result issueListResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decoding sprint issues: %w", err)
		}

		for _, ji := range result.Issues {
			assigneeID := ""
			if ji.Fields.Assignee != nil {
				assigneeID = ji.Fields.Assignee.AccountID
			}
			issues = append(issues, tracker.Issue{
				Key:        ji.Key,
				Summary:    ji.Fields.Summary,
				AssigneeID: assigneeID,
				Priority:   ji.Fields.Priority.Name,
				Lane:       ji.Fields.Status.Name,
				URL:        fmt.Sprintf("%s/browse/%s", c.baseURL, ji.Key),
			})
		}

		startAt += len(result.Issues)
		if startAt >= result.Total || len(result.Issues) == 0 {
			break
		}
	}

	return issues, nil
}

// GetBoardColumns returns the columns configured on the board.
func (c *Client) GetBoardColumns(ctx context.Context) ([]tracker.BoardColumn, error) {
	url := fmt.Sprintf("%s/rest/agile/1.0/board/%d/configuration", c.baseURL, c.boardID)

	req, err := c.newRequest(ctx, http.MethodGet, url)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching board configuration: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira board config: unexpected status %d", resp.StatusCode)
	}

	var result boardConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding board config: %w", err)
	}

	columns := make([]tracker.BoardColumn, 0, len(result.ColumnConfig.Columns))
	for i, col := range result.ColumnConfig.Columns {
		columns = append(columns, tracker.BoardColumn{
			ID:   fmt.Sprintf("%d", i),
			Name: col.Name,
		})
	}

	return columns, nil
}

// GetBoardMembers returns unique assignees found in the current active sprint.
// This is used during `init` to let the user map Jira accounts to messenger IDs.
func (c *Client) GetBoardMembers(ctx context.Context) ([]BoardMember, error) {
	sprint, err := c.GetActiveSprint(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching active sprint for member list: %w", err)
	}

	issues, err := c.GetSprintIssues(ctx, sprint.ID)
	if err != nil {
		return nil, fmt.Errorf("fetching sprint issues for member list: %w", err)
	}

	seen := make(map[string]bool)
	var members []BoardMember
	for _, issue := range issues {
		if issue.AssigneeID == "" {
			continue
		}
		if seen[issue.AssigneeID] {
			continue
		}
		seen[issue.AssigneeID] = true
		// We stored the raw display name in the URL field trick; we need to re-fetch.
		// Instead, we do a secondary call per unique assignee using the issue fields.
		members = append(members, BoardMember{
			AccountID:   issue.AssigneeID,
			DisplayName: issue.AssigneeID, // placeholder; overridden below
		})
	}

	// Re-enrich display names from raw issue data by making a dedicated pass.
	// We piggyback on the already-fetched issues; display names are in jiraIssue.Fields.Assignee.
	// Since GetSprintIssues doesn't expose display names, we call the raw endpoint once.
	url := fmt.Sprintf(
		"%s/rest/agile/1.0/sprint/%d/issue?maxResults=200&fields=assignee",
		c.baseURL, sprint.ID,
	)
	req, err := c.newRequest(ctx, http.MethodGet, url)
	if err != nil {
		return members, nil // return what we have
	}
	resp, err := c.httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return members, nil
	}
	defer resp.Body.Close()

	var result issueListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return members, nil
	}

	// Build a display name map.
	nameMap := make(map[string]string)
	for _, ji := range result.Issues {
		if ji.Fields.Assignee != nil && ji.Fields.Assignee.AccountID != "" {
			nameMap[ji.Fields.Assignee.AccountID] = ji.Fields.Assignee.DisplayName
		}
	}
	for i, m := range members {
		if name, ok := nameMap[m.AccountID]; ok {
			members[i].DisplayName = name
		}
	}

	return members, nil
}

// newRequest creates an authenticated HTTP request.
func (c *Client) newRequest(ctx context.Context, method, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.email, c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return req, nil
}
