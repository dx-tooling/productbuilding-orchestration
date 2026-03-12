package targets

import "testing"

func TestRegistry_GetBySlackChannel(t *testing.T) {
	r := NewRegistry()
	r.targets["acme/app"] = TargetConfig{
		RepoOwner:    "acme",
		RepoName:     "app",
		SlackChannel: "C0PRODUCT",
		GitHubPAT:    "ghp_abc",
	}

	tc, ok := r.GetBySlackChannel("C0PRODUCT")
	if !ok {
		t.Fatal("expected to find target by slack channel")
	}
	if tc.RepoOwner != "acme" || tc.RepoName != "app" {
		t.Errorf("got %s/%s, want acme/app", tc.RepoOwner, tc.RepoName)
	}
}

func TestRegistry_GetBySlackChannel_NotFound(t *testing.T) {
	r := NewRegistry()
	r.targets["acme/app"] = TargetConfig{
		RepoOwner:    "acme",
		RepoName:     "app",
		SlackChannel: "C0OTHER",
	}

	_, ok := r.GetBySlackChannel("C0UNKNOWN")
	if ok {
		t.Error("expected not found for unknown channel")
	}
}
