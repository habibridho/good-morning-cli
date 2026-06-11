package planner

import (
	"sort"
	"unicode"

	"github.com/habib-ridho/good-morning/internal/messenger"
	"github.com/habib-ridho/good-morning/internal/tracker"
)

// Assignment holds one team member and their top-priority issue for the morning.
type Assignment struct {
	Member messenger.Member
	Issue  *tracker.Issue // nil if the member has no assigned issues
}

// MorningPlan is the output of the planner: one assignment per team member.
type MorningPlan struct {
	Sprint      tracker.Sprint
	Assignments []Assignment
}

// LaneMapping maps raw Jira column names to canonical LaneGroups.
// e.g. {"Dev Done": "Ready to Deploy", "Coding": "In Progress"}
type LaneMapping map[string]tracker.LaneGroup

// laneRank returns the priority rank of a given Jira lane (lower = more urgent).
// Unmapped lanes get rank 99.
func laneRank(lane string, mapping LaneMapping) int {
	group, ok := mapping[lane]
	if !ok {
		// Not mapped by user; also try direct match to group names
		group = tracker.LaneGroup(lane)
	}

	for i, g := range tracker.LaneGroupOrder {
		if g == group {
			return i
		}
	}
	return 99
}

// priorityRank extracts a numeric rank from a Jira priority string.
// Examples:
//
//	"P0" → 0, "P1" → 1, "Highest" → 0, "High" → 1, "Medium" → 2, "Low" → 3, "Lowest" → 4
//
// Lower number = higher priority.
func priorityRank(priority string) int {
	// Try extracting a digit first (P0, P1, P2, ...)
	for _, ch := range priority {
		if unicode.IsDigit(ch) {
			return int(ch - '0')
		}
	}

	// Fallback to named priority levels
	switch priority {
	case "Highest", "Critical", "Blocker":
		return 0
	case "High":
		return 1
	case "Medium", "Normal":
		return 2
	case "Low":
		return 3
	case "Lowest", "Minor", "Trivial":
		return 4
	}

	return 99
}

// Build creates a MorningPlan from a list of sprint issues and team members.
// Each member is assigned their single highest-priority issue.
func Build(sprint tracker.Sprint, issues []tracker.Issue, members []messenger.Member, mapping LaneMapping) MorningPlan {
	// Index issues by assignee ID
	byAssignee := make(map[string][]tracker.Issue)
	for _, issue := range issues {
		if issue.AssigneeID != "" {
			byAssignee[issue.AssigneeID] = append(byAssignee[issue.AssigneeID], issue)
		}
	}

	assignments := make([]Assignment, 0, len(members))
	for _, member := range members {
		memberIssues := byAssignee[member.TrackerUserID]

		if len(memberIssues) == 0 {
			assignments = append(assignments, Assignment{Member: member, Issue: nil})
			continue
		}

		// Sort by: lane rank asc, then priority rank asc
		sort.Slice(memberIssues, func(i, j int) bool {
			laneI := laneRank(memberIssues[i].Lane, mapping)
			laneJ := laneRank(memberIssues[j].Lane, mapping)
			if laneI != laneJ {
				return laneI < laneJ
			}
			return priorityRank(memberIssues[i].Priority) < priorityRank(memberIssues[j].Priority)
		})

		top := memberIssues[0]
		assignments = append(assignments, Assignment{Member: member, Issue: &top})
	}

	return MorningPlan{
		Sprint:      sprint,
		Assignments: assignments,
	}
}
