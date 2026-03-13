package domain

import (
	"strings"
	"testing"
)

func TestRenderSystemPrompt(t *testing.T) {
	result, err := RenderSystemPrompt(PromptData{
		RepoOwner: "acme",
		RepoName:  "widgets",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "acme/widgets") {
		t.Errorf("expected prompt to contain repo identifier, got:\n%s", result)
	}
	if !strings.Contains(result, "ProductBuilder") {
		t.Errorf("expected prompt to contain agent name")
	}
	if !strings.Contains(result, "/opencode") {
		t.Errorf("expected prompt to mention /opencode")
	}
}
