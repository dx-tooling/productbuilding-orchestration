package targets

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// TargetConfig holds credentials for a single target repository.
type TargetConfig struct {
	RepoOwner     string `json:"repo_owner"`
	RepoName      string `json:"repo_name"`
	GitHubPAT     string `json:"github_pat"`
	WebhookSecret string `json:"webhook_secret"`

	// Optional Slack integration
	SlackChannel  string `json:"slack_channel,omitempty"`
	SlackBotToken string `json:"slack_bot_token,omitempty"`
}

// Registry provides lookup of target repo configurations.
type Registry struct {
	targets map[string]TargetConfig // key: "owner/repo"
}

func NewRegistry() *Registry {
	return &Registry{targets: make(map[string]TargetConfig)}
}

// Get returns the config for a target repo, if registered.
func (r *Registry) Get(repoOwner, repoName string) (TargetConfig, bool) {
	key := repoOwner + "/" + repoName
	t, ok := r.targets[key]
	return t, ok
}

// GetByChannelName returns the config for a target whose repo name matches
// the Slack channel naming convention "productbuilding-<reponame>".
func (r *Registry) GetByChannelName(channelName string) (TargetConfig, bool) {
	if !strings.HasPrefix(channelName, "productbuilding-") {
		return TargetConfig{}, false
	}
	repoName := strings.TrimPrefix(channelName, "productbuilding-")
	for _, t := range r.targets {
		if t.RepoName == repoName {
			return t, true
		}
	}
	return TargetConfig{}, false
}

// AnyBotToken returns the first available Slack bot token from any target.
func (r *Registry) AnyBotToken() string {
	for _, t := range r.targets {
		if t.SlackBotToken != "" {
			return t.SlackBotToken
		}
	}
	return ""
}

// LoadFromFile reads a JSON array of target configs (as written by cloud-init).
func (r *Registry) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read targets file: %w", err)
	}

	var targets []TargetConfig
	if err := json.Unmarshal(data, &targets); err != nil {
		return fmt.Errorf("parse targets file: %w", err)
	}

	for _, tc := range targets {
		key := tc.RepoOwner + "/" + tc.RepoName
		r.targets[key] = tc
		slog.Info("loaded target config", "target", key)
	}

	return nil
}

// Count returns the number of registered targets.
func (r *Registry) Count() int {
	return len(r.targets)
}
