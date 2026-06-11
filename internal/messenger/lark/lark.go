package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/habib-ridho/good-morning/internal/messenger"
)

const defaultBaseURL = "https://open.larksuite.com/open-apis"

// Client is a Lark messaging client.
type Client struct {
	appID       string
	appSecret   string
	chatID      string
	baseURL     string
	accessToken string
	httpClient  *http.Client
}

// NewClient creates a new Lark client.
func NewClient(appID, appSecret, chatID string) *Client {
	return NewClientWithBaseURL(appID, appSecret, chatID, defaultBaseURL)
}

// NewClientWithBaseURL creates a Lark client with a custom base URL (useful for testing).
func NewClientWithBaseURL(appID, appSecret, chatID, baseURL string) *Client {
	return &Client{
		appID:     appID,
		appSecret: appSecret,
		chatID:    chatID,
		baseURL:   baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// --- Auth ---

type tokenResponse struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
}

// Authenticate obtains a tenant access token from Lark.
func (c *Client) Authenticate() error {
	body, _ := json.Marshal(map[string]string{
		"app_id":     c.appID,
		"app_secret": c.appSecret,
	})

	resp, err := c.httpClient.Post(
		c.baseURL+"/auth/v3/tenant_access_token/internal",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("requesting tenant access token: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("decoding token response: %w", err)
	}
	if tokenResp.Code != 0 {
		return fmt.Errorf("lark auth error (code=%d): %s", tokenResp.Code, tokenResp.Msg)
	}

	c.accessToken = tokenResp.TenantAccessToken
	return nil
}

// --- Message types ---

type postContent struct {
	Tag    string `json:"tag"`
	Text   string `json:"text,omitempty"`
	Href   string `json:"href,omitempty"`
	UserID string `json:"user_id,omitempty"`
}

// --- Messenger interface ---

// SendMorningPlan implements messenger.Messenger.
// It sends the LLM-generated message followed by one line per member
// with their task, using @mentions.
func (c *Client) SendMorningPlan(_ context.Context, message string, _ []messenger.Member) error {
	// The LLM message is the full formatted content (it includes mentions).
	// We send it as a plain text "post" message.
	sections := [][]postContent{
		{{Tag: "text", Text: message}},
	}

	return c.sendGroupMessage("🌅 Good Morning!", sections)
}

// sendGroupMessage sends a rich Lark "post" message to the group chat.
func (c *Client) sendGroupMessage(title string, sections [][]postContent) error {
	content := map[string]interface{}{
		"en_us": map[string]interface{}{
			"title":   title,
			"content": sections,
		},
	}

	contentJSON, _ := json.Marshal(content)

	payload := map[string]interface{}{
		"receive_id": c.chatID,
		"msg_type":   "post",
		"content":    string(contentJSON),
	}

	body, _ := json.Marshal(payload)

	url := c.baseURL + "/im/v1/messages?receive_id_type=chat_id"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building message request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending lark message: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("decoding send response: %w", err)
	}
	if result.Code != 0 {
		return fmt.Errorf("lark send error (code=%d): %s", result.Code, result.Msg)
	}

	return nil
}

// textTag creates a text content element.
func textTag(text string) postContent {
	return postContent{Tag: "text", Text: text}
}

// mentionTag creates an @mention content element.
func mentionTag(userID string) postContent {
	return postContent{Tag: "at", UserID: userID}
}

// linkTag creates a hyperlink content element.
func linkTag(text, href string) postContent {
	return postContent{Tag: "a", Text: text, Href: href}
}

// Ensure unused helper suppression.
var _ = textTag
var _ = mentionTag
var _ = linkTag
