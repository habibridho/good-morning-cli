package planner

import (
	"testing"

	"github.com/habib-ridho/good-morning/internal/messenger"
	"github.com/habib-ridho/good-morning/internal/tracker"
)

func TestLaneRank(t *testing.T) {
	mapping := LaneMapping{
		"Dev Done":    tracker.LaneGroupReadyToDeploy,
		"In QA":       tracker.LaneGroupTesting,
		"Code Review": tracker.LaneGroupInReview,
	}

	tests := []struct {
		lane string
		want int
	}{
		{"Dev Done", 0},              // Mapped
		{"In QA", 1},                 // Mapped
		{"Code Review", 3},           // Mapped
		{"Ready to Deploy", 0},       // Direct match fallback
		{"In Progress", 4},           // Direct match fallback
		{"To Do", 5},                 // Direct match fallback
		{"Random Column", 99},        // Unmapped
	}

	for _, tt := range tests {
		t.Run(tt.lane, func(t *testing.T) {
			if got := laneRank(tt.lane, mapping); got != tt.want {
				t.Errorf("laneRank(%q) = %d, want %d", tt.lane, got, tt.want)
			}
		})
	}
}

func TestPriorityRank(t *testing.T) {
	tests := []struct {
		priority string
		want     int
	}{
		{"P0", 0},
		{"P1", 1},
		{"P2", 2},
		{"P3", 3},
		{"Highest", 0},
		{"High", 1},
		{"Medium", 2},
		{"Normal", 2},
		{"Low", 3},
		{"Lowest", 4},
		{"Unknown", 99},
	}

	for _, tt := range tests {
		t.Run(tt.priority, func(t *testing.T) {
			if got := priorityRank(tt.priority); got != tt.want {
				t.Errorf("priorityRank(%q) = %d, want %d", tt.priority, got, tt.want)
			}
		})
	}
}

func TestBuild(t *testing.T) {
	sprint := tracker.Sprint{ID: 1, Name: "Sprint 1"}

	mAlice := messenger.Member{TrackerUserID: "alice1", MessengerUserID: "lark_alice", Name: "Alice"}
	mBob := messenger.Member{TrackerUserID: "bob2", MessengerUserID: "lark_bob", Name: "Bob"}
	mCharlie := messenger.Member{TrackerUserID: "charlie3", MessengerUserID: "lark_charlie", Name: "Charlie"}
	members := []messenger.Member{mAlice, mBob, mCharlie}

	mapping := LaneMapping{
		"Dev Done": tracker.LaneGroupReadyToDeploy,
	}

	// Alice has two tasks, one in Dev Done (high lane priority), one in To Do
	issueAlice1 := tracker.Issue{Key: "A-1", AssigneeID: "alice1", Lane: "To Do", Priority: "Highest"}
	issueAlice2 := tracker.Issue{Key: "A-2", AssigneeID: "alice1", Lane: "Dev Done", Priority: "Low"} // Should win because Lane is more important

	// Bob has two tasks in the same lane, different priorities
	issueBob1 := tracker.Issue{Key: "B-1", AssigneeID: "bob2", Lane: "In Progress", Priority: "Medium"}
	issueBob2 := tracker.Issue{Key: "B-2", AssigneeID: "bob2", Lane: "In Progress", Priority: "High"} // Should win because priority is higher (1 < 2)

	// Charlie has no tasks

	issues := []tracker.Issue{issueAlice1, issueAlice2, issueBob1, issueBob2}

	plan := Build(sprint, issues, members, mapping)

	if len(plan.Assignments) != 3 {
		t.Fatalf("expected 3 assignments, got %d", len(plan.Assignments))
	}

	for _, a := range plan.Assignments {
		switch a.Member.Name {
		case "Alice":
			if a.Issue == nil || a.Issue.Key != "A-2" {
				t.Errorf("Alice assignment: expected A-2, got %v", a.Issue)
			}
		case "Bob":
			if a.Issue == nil || a.Issue.Key != "B-2" {
				t.Errorf("Bob assignment: expected B-2, got %v", a.Issue)
			}
		case "Charlie":
			if a.Issue != nil {
				t.Errorf("Charlie assignment: expected nil, got %v", a.Issue)
			}
		}
	}
}
