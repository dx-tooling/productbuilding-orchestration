package targets

import (
	"os"
	"testing"
)

func TestRegistry_GetByChannelName(t *testing.T) {
	r := NewRegistry("productbuilding-")
	r.targets["acme/my-cool-app"] = TargetConfig{
		RepoOwner: "acme",
		RepoName:  "my-cool-app",
		GitHubPAT: "ghp_abc",
	}

	tc, ok := r.GetByChannelName("productbuilding-my-cool-app")
	if !ok {
		t.Fatal("expected to find target by channel name convention")
	}
	if tc.RepoOwner != "acme" || tc.RepoName != "my-cool-app" {
		t.Errorf("got %s/%s, want acme/my-cool-app", tc.RepoOwner, tc.RepoName)
	}
}

func TestRegistry_GetByChannelName_NotFound(t *testing.T) {
	r := NewRegistry("productbuilding-")
	r.targets["acme/app"] = TargetConfig{
		RepoOwner: "acme",
		RepoName:  "app",
	}

	_, ok := r.GetByChannelName("productbuilding-unknown-repo")
	if ok {
		t.Error("expected not found for unregistered repo")
	}
}

func TestTargetConfig_BotGitHubLogin(t *testing.T) {
	tc := TargetConfig{
		RepoOwner:      "acme",
		RepoName:       "widgets",
		BotGitHubLogin: "PrdctBldr",
	}
	if tc.BotGitHubLogin != "PrdctBldr" {
		t.Errorf("BotGitHubLogin = %q, want %q", tc.BotGitHubLogin, "PrdctBldr")
	}
}

func TestTargetConfig_BotGitHubLogin_LoadFromFile(t *testing.T) {
	// Verify BotGitHubLogin is populated from JSON config
	dir := t.TempDir()
	path := dir + "/targets.json"
	data := `[{"repo_owner":"acme","repo_name":"widgets","github_pat":"pat","webhook_secret":"sec","bot_github_login":"PrdctBldr"}]`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	r := NewRegistry("productbuilding-")
	if err := r.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	tc, ok := r.Get("acme", "widgets")
	if !ok {
		t.Fatal("expected to find target")
	}
	if tc.BotGitHubLogin != "PrdctBldr" {
		t.Errorf("BotGitHubLogin = %q, want %q", tc.BotGitHubLogin, "PrdctBldr")
	}
}

func TestRegistry_GetByChannelName_WrongPrefix(t *testing.T) {
	r := NewRegistry("productbuilding-")
	r.targets["acme/app"] = TargetConfig{
		RepoOwner: "acme",
		RepoName:  "app",
	}

	_, ok := r.GetByChannelName("random-channel")
	if ok {
		t.Error("expected not found for channel without productbuilding- prefix")
	}
}
