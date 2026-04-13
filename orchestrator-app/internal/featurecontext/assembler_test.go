package featurecontext

import (
	"context"
	"errors"
	"testing"
)

// --- Mock implementations ---

type mockIssueGetter struct {
	issue *IssueState
	err   error
}

func (m *mockIssueGetter) GetIssue(ctx context.Context, owner, repo string, number int, pat string) (*IssueState, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.issue, nil
}

type mockPRGetter struct {
	pr  *PRState
	err error
}

func (m *mockPRGetter) GetPR(ctx context.Context, owner, repo string, number int, pat string) (*PRState, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.pr, nil
}

type mockCheckRunGetter struct {
	checks []CheckRunState
	err    error
}

func (m *mockCheckRunGetter) GetCheckRunsForRef(ctx context.Context, owner, repo, ref, pat string) ([]CheckRunState, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.checks, nil
}

type mockPreviewGetter struct {
	preview *PreviewState
	err     error
}

func (m *mockPreviewGetter) GetPreview(ctx context.Context, owner, repo string, prNumber int) (*PreviewState, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.preview, nil
}

// --- Tests ---

func TestAssembler_ForPR_FullContext(t *testing.T) {
	a := NewAssembler(
		&mockIssueGetter{issue: &IssueState{Number: 5, Title: "Add dark mode", Body: "Please", State: "open"}},
		&mockPRGetter{pr: &PRState{Number: 10, Title: "Dark mode PR", State: "open", HeadSHA: "abc123", HeadRef: "feature", Author: "alice", Additions: 50, Deletions: 10, URL: "https://github.com/acme/widgets/pull/10"}},
		&mockCheckRunGetter{checks: []CheckRunState{
			{Name: "build", Conclusion: "success", URL: "https://github.com/runs/1"},
			{Name: "lint", Conclusion: "failure", URL: "https://github.com/runs/2"},
		}},
		&mockPreviewGetter{preview: &PreviewState{Status: "ready", URL: "https://preview.example.com"}},
	)

	snap, err := a.ForPR(context.Background(), "acme", "widgets", "ghp_test", 10, 5)
	if err != nil {
		t.Fatalf("ForPR() error = %v", err)
	}

	if snap.PR.Number != 10 {
		t.Errorf("PR.Number = %d, want 10", snap.PR.Number)
	}
	if snap.Issue.Number != 5 {
		t.Errorf("Issue.Number = %d, want 5", snap.Issue.Number)
	}
	if snap.CIStatus != CIFailing {
		t.Errorf("CIStatus = %q, want %q", snap.CIStatus, CIFailing)
	}
	if snap.Preview.Status != "ready" {
		t.Errorf("Preview.Status = %q, want %q", snap.Preview.Status, "ready")
	}
	if len(snap.CIDetails) != 2 {
		t.Errorf("len(CIDetails) = %d, want 2", len(snap.CIDetails))
	}
}

func TestAssembler_ForPR_NoLinkedIssue(t *testing.T) {
	a := NewAssembler(
		&mockIssueGetter{issue: &IssueState{Number: 5}},
		&mockPRGetter{pr: &PRState{Number: 10, HeadSHA: "abc"}},
		&mockCheckRunGetter{checks: nil},
		&mockPreviewGetter{preview: nil},
	)

	snap, err := a.ForPR(context.Background(), "acme", "widgets", "ghp_test", 10, 0)
	if err != nil {
		t.Fatalf("ForPR() error = %v", err)
	}

	if snap.Issue != nil {
		t.Error("Issue should be nil when linkedIssue=0")
	}
	if snap.PR == nil {
		t.Fatal("PR should be populated")
	}
	if snap.PR.Number != 10 {
		t.Errorf("PR.Number = %d, want 10", snap.PR.Number)
	}
}

func TestAssembler_ForPR_NoPreview(t *testing.T) {
	a := NewAssembler(
		&mockIssueGetter{},
		&mockPRGetter{pr: &PRState{Number: 10, HeadSHA: "abc"}},
		&mockCheckRunGetter{checks: nil},
		&mockPreviewGetter{preview: nil},
	)

	snap, err := a.ForPR(context.Background(), "acme", "widgets", "ghp_test", 10, 0)
	if err != nil {
		t.Fatalf("ForPR() error = %v", err)
	}

	if snap.Preview != nil {
		t.Error("Preview should be nil")
	}
}

func TestAssembler_ForPR_NoCheckRuns(t *testing.T) {
	a := NewAssembler(
		&mockIssueGetter{},
		&mockPRGetter{pr: &PRState{Number: 10, HeadSHA: "abc"}},
		&mockCheckRunGetter{checks: []CheckRunState{}},
		&mockPreviewGetter{preview: nil},
	)

	snap, err := a.ForPR(context.Background(), "acme", "widgets", "ghp_test", 10, 0)
	if err != nil {
		t.Fatalf("ForPR() error = %v", err)
	}

	if snap.CIStatus != CIUnknown {
		t.Errorf("CIStatus = %q, want %q", snap.CIStatus, CIUnknown)
	}
}

func TestAssembler_ForIssue_Basic(t *testing.T) {
	a := NewAssembler(
		&mockIssueGetter{issue: &IssueState{Number: 42, Title: "Bug report", Body: "It's broken", State: "open"}},
		&mockPRGetter{},
		&mockCheckRunGetter{},
		&mockPreviewGetter{},
	)

	snap, err := a.ForIssue(context.Background(), "acme", "widgets", "ghp_test", 42)
	if err != nil {
		t.Fatalf("ForIssue() error = %v", err)
	}

	if snap.Issue == nil {
		t.Fatal("Issue should be populated")
	}
	if snap.Issue.Number != 42 {
		t.Errorf("Issue.Number = %d, want 42", snap.Issue.Number)
	}
	if snap.Issue.State != "open" {
		t.Errorf("Issue.State = %q, want %q", snap.Issue.State, "open")
	}
	if snap.PR != nil {
		t.Error("PR should be nil for issue-only snapshot")
	}
	if snap.CIStatus != CIUnknown {
		t.Errorf("CIStatus = %q, want %q", snap.CIStatus, CIUnknown)
	}
	if snap.Preview != nil {
		t.Error("Preview should be nil for issue-only snapshot")
	}
}

func TestAssembler_ForPR_IssueGetterError_Nonfatal(t *testing.T) {
	a := NewAssembler(
		&mockIssueGetter{err: errors.New("issue not found")},
		&mockPRGetter{pr: &PRState{Number: 10, Title: "My PR", HeadSHA: "abc"}},
		&mockCheckRunGetter{checks: nil},
		&mockPreviewGetter{preview: nil},
	)

	snap, err := a.ForPR(context.Background(), "acme", "widgets", "ghp_test", 10, 5)
	if err != nil {
		t.Fatalf("ForPR() should not return error when IssueGetter fails, got %v", err)
	}

	if snap.Issue != nil {
		t.Error("Issue should be nil when IssueGetter returns error")
	}
	if snap.PR == nil {
		t.Fatal("PR should still be populated")
	}
	if snap.PR.Number != 10 {
		t.Errorf("PR.Number = %d, want 10", snap.PR.Number)
	}
}

func TestAssembler_CIStatus_AllPassing(t *testing.T) {
	a := NewAssembler(
		&mockIssueGetter{},
		&mockPRGetter{pr: &PRState{Number: 10, HeadSHA: "abc"}},
		&mockCheckRunGetter{checks: []CheckRunState{
			{Name: "build", Conclusion: "success"},
			{Name: "lint", Conclusion: "success"},
			{Name: "test", Conclusion: "success"},
		}},
		&mockPreviewGetter{preview: nil},
	)

	snap, err := a.ForPR(context.Background(), "acme", "widgets", "ghp_test", 10, 0)
	if err != nil {
		t.Fatalf("ForPR() error = %v", err)
	}

	if snap.CIStatus != CIPassing {
		t.Errorf("CIStatus = %q, want %q", snap.CIStatus, CIPassing)
	}
}

func TestAssembler_CIStatus_Pending(t *testing.T) {
	a := NewAssembler(
		&mockIssueGetter{},
		&mockPRGetter{pr: &PRState{Number: 10, HeadSHA: "abc"}},
		&mockCheckRunGetter{checks: []CheckRunState{
			{Name: "build", Conclusion: "success"},
			{Name: "lint", Conclusion: ""}, // still running
		}},
		&mockPreviewGetter{preview: nil},
	)

	snap, err := a.ForPR(context.Background(), "acme", "widgets", "ghp_test", 10, 0)
	if err != nil {
		t.Fatalf("ForPR() error = %v", err)
	}

	if snap.CIStatus != CIPending {
		t.Errorf("CIStatus = %q, want %q", snap.CIStatus, CIPending)
	}
}

func TestAssembler_CIStatus_MixedFailAndPending(t *testing.T) {
	a := NewAssembler(
		&mockIssueGetter{},
		&mockPRGetter{pr: &PRState{Number: 10, HeadSHA: "abc"}},
		&mockCheckRunGetter{checks: []CheckRunState{
			{Name: "build", Conclusion: "failure"},
			{Name: "lint", Conclusion: ""}, // still running
		}},
		&mockPreviewGetter{preview: nil},
	)

	snap, err := a.ForPR(context.Background(), "acme", "widgets", "ghp_test", 10, 0)
	if err != nil {
		t.Fatalf("ForPR() error = %v", err)
	}

	if snap.CIStatus != CIFailing {
		t.Errorf("CIStatus = %q, want %q (failure takes precedence)", snap.CIStatus, CIFailing)
	}
}
