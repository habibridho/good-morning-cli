package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// SendMorningPlan implements messenger.Messenger.
// It sends the LLM-generated message using an interactive card.
func (c *Client) SendMorningPlan(_ context.Context, message string, members []messenger.Member) error {
	// Replace mentions in the markdown text
	for _, m := range members {
		if m.Name != "" && m.MessengerUserID != "" {
			mention := fmt.Sprintf("<at id=\"%s\"></at>", m.MessengerUserID)
			// Replace @Name first, then Name, to avoid double replacing if LLM outputted @Name
			message = strings.ReplaceAll(message, "@"+m.Name, mention)
			message = strings.ReplaceAll(message, m.Name, mention)
		}
	}

	return c.sendInteractiveCard("🌅 Good Morning!", message)
}

// sendInteractiveCard sends a rich Lark "interactive" card message to the group chat.
func (c *Client) sendInteractiveCard(title string, message string) error {
	card := map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"header": map[string]interface{}{
			"title": map[string]interface{}{
				"tag":     "plain_text",
				"content": title,
			},
			"template": "blue",
		},
		"elements": []interface{}{
			map[string]interface{}{
				"tag":     "markdown",
				"content": message,
			},
		},
	}

	contentJSON, _ := json.Marshal(card)

	payload := map[string]interface{}{
		"receive_id": c.chatID,
		"msg_type":   "interactive",
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

// GetUserIDsByEmails looks up Lark User IDs by email addresses.
// It returns a map of email to user_id.
func (c *Client) GetUserIDsByEmails(ctx context.Context, emails []string) (map[string]string, error) {
	if len(emails) == 0 {
		return nil, nil
	}

	payload := map[string]interface{}{
		"emails": emails,
	}
	body, _ := json.Marshal(payload)

	url := c.baseURL + "/contact/v3/users/batch_get_id?user_id_type=user_id"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building batch_get_id request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling batch_get_id: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			UserList []struct {
				UserID string `json:"user_id"`
				Email  string `json:"email"`
			} `json:"user_list"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decoding batch_get_id response: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("lark batch_get_id error (code=%d): %s", result.Code, result.Msg)
	}

	res := make(map[string]string)
	for _, u := range result.Data.UserList {
		if u.UserID != "" && u.Email != "" {
			res[u.Email] = u.UserID
		}
	}

	return res, nil
}

