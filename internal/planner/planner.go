package planner

import (
	"sort"
	"strings"
	"unicode"

	"github.com/habib-ridho/good-morning/internal/messenger"
	"github.com/habib-ridho/good-morning/internal/tracker"
)

// Reason constants for assignment
const (
	ReasonAssignee = "Assignee"
	ReasonToReview = "To Review"
	ReasonToTest   = "To Test"
)

// Assignment holds one team member and their top-priority issue for the morning.
type Assignment struct {
	Member messenger.Member
	Issue  *tracker.Issue // nil if the member has no assigned issues
	Reason string         // e.g., ReasonAssignee, ReasonToReview, ReasonToTest
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

func issueLess(a, b tracker.Issue, mapping LaneMapping) bool {
	laneA := laneRank(a.Lane, mapping)
	laneB := laneRank(b.Lane, mapping)
	if laneA != laneB {
		return laneA < laneB
	}
	return priorityRank(a.Priority) < priorityRank(b.Priority)
}

func getLaneGroup(lane string, mapping LaneMapping) tracker.LaneGroup {
	if group, ok := mapping[lane]; ok {
		return group
	}
	return tracker.LaneGroup(lane)
}

// Build creates a MorningPlan from a list of sprint issues and team members.
// Each member is assigned their single highest-priority issue based on role and lanes.
func Build(sprint tracker.Sprint, issues []tracker.Issue, members []messenger.Member, mapping LaneMapping) MorningPlan {
	var developers []messenger.Member
	var testers []messenger.Member

	for _, m := range members {
		role := strings.ToLower(m.Role)
		if role == "tester" || role == "qa" {
			testers = append(testers, m)
		} else {
			developers = append(developers, m)
		}
	}

	var testingIssues []tracker.Issue
	var reviewIssues []tracker.Issue
	var workIssues []tracker.Issue

	for _, issue := range issues {
		group := getLaneGroup(issue.Lane, mapping)
		if group == tracker.LaneGroupTesting || group == tracker.LaneGroupReadyToTest {
			testingIssues = append(testingIssues, issue)
		} else if group == tracker.LaneGroupInReview {
			reviewIssues = append(reviewIssues, issue)
		} else {
			workIssues = append(workIssues, issue)
		}
	}

	sort.Slice(testingIssues, func(i, j int) bool { return issueLess(testingIssues[i], testingIssues[j], mapping) })
	sort.Slice(reviewIssues, func(i, j int) bool { return issueLess(reviewIssues[i], reviewIssues[j], mapping) })

	// Index work issues by AssigneeID
	workByAssignee := make(map[string][]tracker.Issue)
	for _, issue := range workIssues {
		if issue.AssigneeID != "" {
			workByAssignee[issue.AssigneeID] = append(workByAssignee[issue.AssigneeID], issue)
		}
	}
	for _, devIssues := range workByAssignee {
		sort.Slice(devIssues, func(i, j int) bool { return issueLess(devIssues[i], devIssues[j], mapping) })
	}

	assignments := make([]Assignment, 0, len(members))

	// Map of member ID -> pointer to Assignment (to allow updates)
	devAssignments := make(map[string]*Assignment)

	// Preliminary Dev Assignments
	for _, dev := range developers {
		var topIssue *tracker.Issue
		if issuesList, ok := workByAssignee[dev.TrackerUserID]; ok && len(issuesList) > 0 {
			topIssue = &issuesList[0]
		}
		a := Assignment{Member: dev, Issue: topIssue, Reason: ReasonAssignee}
		devAssignments[dev.TrackerUserID] = &a
	}

	// Assign Review issues
	for _, reviewIssue := range reviewIssues {
		var bestDevID string
		// 1. First, find a "free" developer
		for _, dev := range developers {
			if dev.TrackerUserID == reviewIssue.AssigneeID {
				continue
			}
			a := devAssignments[dev.TrackerUserID]
			if a.Issue == nil {
				bestDevID = dev.TrackerUserID
				break
			}
		}

		// 2. If no free dev, find a busy developer working on a lower priority issue
		if bestDevID == "" {
			var worstDevID string
			for _, dev := range developers {
				if dev.TrackerUserID == reviewIssue.AssigneeID {
					continue
				}
				a := devAssignments[dev.TrackerUserID]
				if a.Issue != nil {
					// Is the review issue more important than what they are currently doing?
					if issueLess(reviewIssue, *a.Issue, mapping) {
						if worstDevID == "" {
							worstDevID = dev.TrackerUserID
						} else {
							// Prefer to interrupt the dev with the even lower priority task
							worstAssignment := devAssignments[worstDevID]
							if issueLess(*worstAssignment.Issue, *a.Issue, mapping) {
								worstDevID = dev.TrackerUserID
							}
						}
					}
				}
			}
			if worstDevID != "" {
				bestDevID = worstDevID
			}
		}

		if bestDevID != "" {
			a := devAssignments[bestDevID]
			ri := reviewIssue // copy for pointer
			a.Issue = &ri
			a.Reason = ReasonToReview
		}
	}

	for _, dev := range developers {
		assignments = append(assignments, *devAssignments[dev.TrackerUserID])
	}

	// Assign Tester issues
	if len(testers) > 0 {
		for i, tester := range testers {
			var topIssue *tracker.Issue
			if i < len(testingIssues) {
				ti := testingIssues[i]
				topIssue = &ti
			}
			assignments = append(assignments, Assignment{
				Member: tester,
				Issue:  topIssue,
				Reason: ReasonToTest,
			})
		}
	}

	return MorningPlan{
		Sprint:      sprint,
		Assignments: assignments,
	}
}
