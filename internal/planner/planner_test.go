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

	mAlice := messenger.Member{TrackerUserID: "alice1", MessengerUserID: "lark_alice", Name: "Alice", Role: "developer"}
	mBob := messenger.Member{TrackerUserID: "bob2", MessengerUserID: "lark_bob", Name: "Bob", Role: "developer"}
	mCharlie := messenger.Member{TrackerUserID: "charlie3", MessengerUserID: "lark_charlie", Name: "Charlie", Role: "developer"}
	mDave := messenger.Member{TrackerUserID: "dave4", MessengerUserID: "lark_dave", Name: "Dave", Role: "tester"}
	mEve := messenger.Member{TrackerUserID: "eve5", MessengerUserID: "lark_eve", Name: "Eve", Role: "tester"}
	members := []messenger.Member{mAlice, mBob, mCharlie, mDave, mEve}

	mapping := LaneMapping{
		"Dev Done": tracker.LaneGroupReadyToDeploy,
	}

	// Alice has a task in Dev Done (Assignee)
	issueAlice1 := tracker.Issue{Key: "A-1", AssigneeID: "alice1", Lane: "To Do", Priority: "Highest"}
	issueAlice2 := tracker.Issue{Key: "A-2", AssigneeID: "alice1", Lane: "Dev Done", Priority: "Low"} // Should win because Lane is more important

	// Bob has no tasks, but there is an "In Review" task assigned to Alice
	issueAlice3 := tracker.Issue{Key: "A-3", AssigneeID: "alice1", Lane: "In Review", Priority: "High"} // Bob should get this to review

	// Charlie is working on an "In Progress" task (P3).
	issueCharlie1 := tracker.Issue{Key: "C-1", AssigneeID: "charlie3", Lane: "In Progress", Priority: "Low"}

	// There's another "In Review" task (P1). Since Charlie's task is P3 "In Progress", Charlie should be interrupted to review this.
	issueCharlie2 := tracker.Issue{Key: "C-2", AssigneeID: "bob2", Lane: "In Review", Priority: "High"} // Actually assignee is bob2, Charlie reviews

	// Testing issues
	issueTest1 := tracker.Issue{Key: "T-1", AssigneeID: "alice1", Lane: "Ready to Test", Priority: "Highest"} // Should go to Dave
	issueTest2 := tracker.Issue{Key: "T-2", AssigneeID: "alice1", Lane: "Testing", Priority: "Low"} // Should go to Eve
	issueTest3 := tracker.Issue{Key: "T-3", AssigneeID: "bob2", Lane: "Ready to Test", Priority: "High"} // Not assigned because only 2 testers

	issues := []tracker.Issue{issueAlice1, issueAlice2, issueAlice3, issueCharlie1, issueCharlie2, issueTest1, issueTest2, issueTest3}

	plan := Build(sprint, issues, members, mapping)

	if len(plan.Assignments) != 5 {
		t.Fatalf("expected 5 assignments, got %d", len(plan.Assignments))
	}

	for _, a := range plan.Assignments {
		switch a.Member.Name {
		case "Alice":
			if a.Issue == nil || a.Issue.Key != "A-2" || a.Reason != ReasonAssignee {
				t.Errorf("Alice assignment: expected A-2 (Assignee), got %v (Reason: %s)", a.Issue, a.Reason)
			}
		case "Bob":
			if a.Issue == nil || a.Issue.Key != "A-3" || a.Reason != ReasonToReview {
				t.Errorf("Bob assignment: expected A-3 (To Review), got %v (Reason: %s)", a.Issue, a.Reason)
			}
		case "Charlie":
			if a.Issue == nil || a.Issue.Key != "C-2" || a.Reason != ReasonToReview {
				t.Errorf("Charlie assignment: expected C-2 (To Review), got %v (Reason: %s)", a.Issue, a.Reason)
			}
		case "Dave":
			if a.Issue == nil || a.Issue.Key != "T-2" || a.Reason != ReasonToTest {
				t.Errorf("Dave assignment: expected T-2 (To Test), got %v (Reason: %s)", a.Issue, a.Reason)
			}
		case "Eve":
			if a.Issue == nil || a.Issue.Key != "T-1" || a.Reason != ReasonToTest {
				t.Errorf("Eve assignment: expected T-1 (To Test), got %v (Reason: %s)", a.Issue, a.Reason)
			}
		}
	}
}
