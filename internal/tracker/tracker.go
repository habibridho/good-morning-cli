package tracker

import "context"

// LaneGroup represents one of the canonical priority framework groups.
// Lower index = closer to production = higher priority.
type LaneGroup string

const (
	LaneGroupReadyToDeploy LaneGroup = "Ready to Deploy"
	LaneGroupTesting       LaneGroup = "Testing"
	LaneGroupReadyToTest   LaneGroup = "Ready to Test"
	LaneGroupInReview      LaneGroup = "In Review"
	LaneGroupInProgress    LaneGroup = "In Progress"
	LaneGroupToDo          LaneGroup = "To Do"
)

// LaneGroupOrder defines the canonical priority order (index 0 = highest priority).
var LaneGroupOrder = []LaneGroup{
	LaneGroupReadyToDeploy,
	LaneGroupTesting,
	LaneGroupReadyToTest,
	LaneGroupInReview,
	LaneGroupInProgress,
	LaneGroupToDo,
}

// Issue represents a work item from a project tracker.
type Issue struct {
	Key        string
	Summary    string
	AssigneeID string // tracker-native user ID (e.g. Jira account ID)
	Priority   string // raw priority name, e.g. "P1", "Highest", "High"
	Lane       string // raw column/status name from the board
	URL        string
}

// Sprint represents an active sprint / iteration.
type Sprint struct {
	ID   int
	Name string
}

// BoardColumn describes a column on the project board.
type BoardColumn struct {
	ID   string
	Name string
}

// ProjectTracker is the interface that abstracts a project management tool
// (Jira, Asana, Trello, etc.).
type ProjectTracker interface {
	// GetActiveSprint returns the currently active sprint for the configured board.
	GetActiveSprint(ctx context.Context) (*Sprint, error)

	// GetSprintIssues returns all issues in the given sprint.
	GetSprintIssues(ctx context.Context, sprintID int) ([]Issue, error)

	// GetBoardColumns returns the columns defined on the configured board.
	// Used during `init` to let the user map columns to lane groups.
	GetBoardColumns(ctx context.Context) ([]BoardColumn, error)
}
