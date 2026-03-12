package targets

import "testing"

func TestRegistry_GetByChannelName(t *testing.T) {
	r := NewRegistry()
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
	r := NewRegistry()
	r.targets["acme/app"] = TargetConfig{
		RepoOwner: "acme",
		RepoName:  "app",
	}

	_, ok := r.GetByChannelName("productbuilding-unknown-repo")
	if ok {
		t.Error("expected not found for unregistered repo")
	}
}

func TestRegistry_GetByChannelName_WrongPrefix(t *testing.T) {
	r := NewRegistry()
	r.targets["acme/app"] = TargetConfig{
		RepoOwner: "acme",
		RepoName:  "app",
	}

	_, ok := r.GetByChannelName("random-channel")
	if ok {
		t.Error("expected not found for channel without productbuilding- prefix")
	}
}
