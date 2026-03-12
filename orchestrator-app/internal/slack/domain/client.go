package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const slackAPIBaseURL = "https://slack.com/api"

// Client provides methods to interact with the Slack API
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new Slack client
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{},
		baseURL:    slackAPIBaseURL,
	}
}

// NewClientWithBaseURL creates a client with a custom base URL (for testing)
func NewClientWithBaseURL(baseURL string) *Client {
	return &Client{
		httpClient: &http.Client{},
		baseURL:    baseURL,
	}
}

// slackResponse represents the common Slack API response structure
type slackResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Ts    string `json:"ts,omitempty"`
}

// PostMessage posts a message to a channel and returns the timestamp
func (c *Client) PostMessage(ctx context.Context, botToken, channel string, msg MessageBlock) (string, error) {
	payload := map[string]interface{}{
		"channel": channel,
		"text":    msg.Text,
	}

	if len(msg.Blocks) > 0 {
		payload["blocks"] = msg.Blocks
	}

	return c.post(ctx, botToken, "/chat.postMessage", payload)
}

// PostToThread posts a message to an existing thread
func (c *Client) PostToThread(ctx context.Context, botToken, channel, threadTs string, msg MessageBlock) error {
	payload := map[string]interface{}{
		"channel":   channel,
		"thread_ts": threadTs,
		"text":      msg.Text,
	}

	if len(msg.Blocks) > 0 {
		payload["blocks"] = msg.Blocks
	}

	_, err := c.post(ctx, botToken, "/chat.postMessage", payload)
	return err
}

// AddReaction adds an emoji reaction to a message
func (c *Client) AddReaction(ctx context.Context, botToken, channel, timestamp, emoji string) error {
	payload := map[string]interface{}{
		"channel":   channel,
		"timestamp": timestamp,
		"name":      emoji,
	}

	_, err := c.post(ctx, botToken, "/reactions.add", payload)
	return err
}

// RemoveReaction removes an emoji reaction from a message
func (c *Client) RemoveReaction(ctx context.Context, botToken, channel, timestamp, emoji string) error {
	payload := map[string]interface{}{
		"channel":   channel,
		"timestamp": timestamp,
		"name":      emoji,
	}

	_, err := c.post(ctx, botToken, "/reactions.remove", payload)
	return err
}

// userInfoResponse represents the Slack users.info API response
type userInfoResponse struct {
	OK   bool `json:"ok"`
	User struct {
		Profile struct {
			DisplayName string `json:"display_name"`
			RealName    string `json:"real_name"`
		} `json:"profile"`
	} `json:"user"`
	Error string `json:"error,omitempty"`
}

// GetUserInfo resolves a Slack user ID to their display name
func (c *Client) GetUserInfo(ctx context.Context, botToken, userID string) (string, error) {
	url := c.baseURL + "/users.info?user=" + userID

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+botToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("slack api request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("slack api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var userResp userInfoResponse
	if err := json.Unmarshal(respBody, &userResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if !userResp.OK {
		return "", fmt.Errorf("slack api error: %s", userResp.Error)
	}

	name := userResp.User.Profile.DisplayName
	if name == "" {
		name = userResp.User.Profile.RealName
	}

	return name, nil
}

// channelInfoResponse represents the Slack conversations.info API response
type channelInfoResponse struct {
	OK      bool `json:"ok"`
	Channel struct {
		Name string `json:"name"`
	} `json:"channel"`
	Error string `json:"error,omitempty"`
}

// GetChannelName resolves a Slack channel ID to its name
func (c *Client) GetChannelName(ctx context.Context, botToken, channelID string) (string, error) {
	url := c.baseURL + "/conversations.info?channel=" + channelID

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+botToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("slack api request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("slack api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var chResp channelInfoResponse
	if err := json.Unmarshal(respBody, &chResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if !chResp.OK {
		return "", fmt.Errorf("slack api error: %s", chResp.Error)
	}

	return chResp.Channel.Name, nil
}

// post makes a POST request to the Slack API
func (c *Client) post(ctx context.Context, botToken, endpoint string, payload interface{}) (string, error) {
	url := c.baseURL + endpoint

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+botToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("slack api request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("slack api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var slackResp slackResponse
	if err := json.Unmarshal(respBody, &slackResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if !slackResp.OK {
		return "", fmt.Errorf("slack api error: %s", slackResp.Error)
	}

	return slackResp.Ts, nil
}
