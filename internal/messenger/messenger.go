package messenger

import "context"

// Member represents a team member with their identity across different systems.
type Member struct {
	Name            string // Display name
	TrackerUserID   string // e.g. Jira account ID
	MessengerUserID string // e.g. Lark user ID for @mention
	Role            string // e.g. "developer" or "tester"
}

// Messenger is the interface that abstracts a messaging platform
// (Lark, Slack, Teams, etc.).
type Messenger interface {
	// SendMorningPlan sends the morning plan message to the team channel.
	// message is the LLM-generated text; members is used for @mentions.
	SendMorningPlan(ctx context.Context, message string, members []Member) error
}
