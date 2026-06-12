package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/habib-ridho/good-morning/internal/planner"
	"github.com/habib-ridho/good-morning/internal/tracker"
)

// DefaultModel is the LLM model used when none is configured.
const DefaultModel = "GoToCompany/llama3-8b-cpt-sahabatai-v1-instruct"
const defaultSystemPrompt = `You are a helpful bot called Good Morning that sends morning briefings to engineering teams.
Your job is to generate a short, friendly morning message for the team that:
1. Greets the team warmly
2. Lists each team member's top priority task for the morning
3. Includes a light-hearted one-liner or encouragement
Keep the message concise, professional yet casual, and motivating.`
const defaultTimeout = 60 * time.Second

// Client interacts with an OpenAI-compatible LLM server.
type Client struct {
	baseURL      string
	model        string
	systemPrompt string
	httpClient   *http.Client
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature"`
	TopP        float64   `json:"top_p"`
	Stream      bool      `json:"stream"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []choice `json:"choices"`
}

type choice struct {
	Message message `json:"message"`
}

// NewClient creates a new LLM client.
func NewClient(baseURL, model, systemPrompt string, timeoutSec int) *Client {
	if model == "" {
		model = DefaultModel
	}
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}
	timeout := defaultTimeout
	if timeoutSec > 0 {
		timeout = time.Duration(timeoutSec) * time.Second
	}
	return &Client{
		baseURL:      baseURL,
		model:        model,
		systemPrompt: systemPrompt,
		httpClient:   &http.Client{Timeout: timeout},
	}
}

// Warmup sends a minimal request to pre-load the model.
func (c *Client) Warmup(ctx context.Context) error {
	reqBody := chatRequest{
		Model:       c.model,
		Messages:    []message{{Role: "user", Content: "hi"}},
		Temperature: 0,
		Stream:      false,
		MaxTokens:   1,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling warmup request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("creating warmup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending warmup request: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	return nil
}

// GenerateMorningMessage creates a morning briefing message from a planner.MorningPlan.
func (c *Client) GenerateMorningMessage(ctx context.Context, plan planner.MorningPlan) (string, error) {
	userPrompt := buildPrompt(plan)
	return c.generate(ctx, userPrompt)
}

// buildPrompt constructs the user prompt from the morning plan.
func buildPrompt(plan planner.MorningPlan) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Sprint: %s\n\n", plan.Sprint.Name))
	sb.WriteString("Team assignments for this morning:\n\n")

	for _, a := range plan.Assignments {
		if a.Issue == nil {
			sb.WriteString(fmt.Sprintf("- %s: No assigned tasks in this sprint\n", a.Member.Name))
			continue
		}
		lane := laneDisplay(a.Issue.Lane)
		reasonMsg := ""
		if a.Reason != "" && a.Reason != planner.ReasonAssignee {
			reasonMsg = fmt.Sprintf(" (%s)", a.Reason)
		}
		sb.WriteString(fmt.Sprintf("- %s%s [%s | %s | %s]: %s (%s)\n",
			a.Member.Name,
			reasonMsg,
			a.Issue.Key,
			lane,
			a.Issue.Priority,
			a.Issue.Summary,
			a.Issue.URL,
		))
	}

	sb.WriteString("\nPlease generate a short morning briefing message for the team. ")
	sb.WriteString("Mention each team member by name. Be warm, concise, and add a short motivational note.")
	return sb.String()
}

// laneDisplay returns a display-friendly lane description.
func laneDisplay(lane string) string {
	switch tracker.LaneGroup(lane) {
	case tracker.LaneGroupReadyToDeploy:
		return "🚀 Ready to Deploy"
	case tracker.LaneGroupTesting:
		return "🧪 Testing"
	case tracker.LaneGroupReadyToTest:
		return "🔍 Ready to Test"
	case tracker.LaneGroupInReview:
		return "👀 In Review"
	case tracker.LaneGroupInProgress:
		return "🔨 In Progress"
	case tracker.LaneGroupToDo:
		return "📋 To Do"
	default:
		return lane
	}
}

// generate calls the LLM completions endpoint.
func (c *Client) generate(ctx context.Context, userPrompt string) (string, error) {
	reqBody := chatRequest{
		Model: c.model,
		Messages: []message{
			{Role: "system", Content: c.systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.6,
		TopP:        0.9,
		Stream:      false,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	content := chatResp.Choices[0].Message.Content
	content = strings.Trim(content, "\"")
	return content, nil
}
