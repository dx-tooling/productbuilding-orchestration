package domain

import (
	"testing"
)

func TestSlackThread_New(t *testing.T) {
	tests := []struct {
		name        string
		repoOwner   string
		repoName    string
		issueNumber int
		prNumber    int
		channel     string
		threadTs    string
		wantType    string
		wantErr     bool
	}{
		{
			name:        "valid issue thread",
			repoOwner:   "luminor-project",
			repoName:    "my-app",
			issueNumber: 42,
			prNumber:    0,
			channel:     "#productbuilding-my-app",
			threadTs:    "1234567890.123456",
			wantType:    "issue",
			wantErr:     false,
		},
		{
			name:        "valid PR thread",
			repoOwner:   "luminor-project",
			repoName:    "my-app",
			issueNumber: 0,
			prNumber:    42,
			channel:     "#productbuilding-my-app",
			threadTs:    "1234567890.123456",
			wantType:    "pull_request",
			wantErr:     false,
		},
		{
			name:        "both issue and PR set should error",
			repoOwner:   "luminor-project",
			repoName:    "my-app",
			issueNumber: 42,
			prNumber:    42,
			channel:     "#productbuilding-my-app",
			threadTs:    "1234567890.123456",
			wantErr:     true,
		},
		{
			name:        "neither issue nor PR should error",
			repoOwner:   "luminor-project",
			repoName:    "my-app",
			issueNumber: 0,
			prNumber:    0,
			channel:     "#productbuilding-my-app",
			threadTs:    "1234567890.123456",
			wantErr:     true,
		},
		{
			name:        "empty repo owner should error",
			repoOwner:   "",
			repoName:    "my-app",
			issueNumber: 42,
			prNumber:    0,
			channel:     "#productbuilding-my-app",
			threadTs:    "1234567890.123456",
			wantErr:     true,
		},
		{
			name:        "empty repo name should error",
			repoOwner:   "luminor-project",
			repoName:    "",
			issueNumber: 42,
			prNumber:    0,
			channel:     "#productbuilding-my-app",
			threadTs:    "1234567890.123456",
			wantErr:     true,
		},
		{
			name:        "empty channel should error",
			repoOwner:   "luminor-project",
			repoName:    "my-app",
			issueNumber: 42,
			prNumber:    0,
			channel:     "",
			threadTs:    "1234567890.123456",
			wantErr:     true,
		},
		{
			name:        "empty thread timestamp should error",
			repoOwner:   "luminor-project",
			repoName:    "my-app",
			issueNumber: 42,
			prNumber:    0,
			channel:     "#productbuilding-my-app",
			threadTs:    "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thread, err := NewSlackThread(tt.repoOwner, tt.repoName, tt.issueNumber, tt.prNumber, tt.channel, tt.threadTs)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewSlackThread() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("NewSlackThread() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if thread.RepoOwner != tt.repoOwner {
				t.Errorf("RepoOwner = %v, want %v", thread.RepoOwner, tt.repoOwner)
			}
			if thread.RepoName != tt.repoName {
				t.Errorf("RepoName = %v, want %v", thread.RepoName, tt.repoName)
			}
			if thread.ThreadType != tt.wantType {
				t.Errorf("ThreadType = %v, want %v", thread.ThreadType, tt.wantType)
			}
			if thread.SlackChannel != tt.channel {
				t.Errorf("SlackChannel = %v, want %v", thread.SlackChannel, tt.channel)
			}
			if thread.SlackThreadTs != tt.threadTs {
				t.Errorf("SlackThreadTs = %v, want %v", thread.SlackThreadTs, tt.threadTs)
			}
			if thread.ID == "" {
				t.Error("ID should not be empty")
			}
			if thread.CreatedAt.IsZero() {
				t.Error("CreatedAt should not be zero")
			}
			if thread.UpdatedAt.IsZero() {
				t.Error("UpdatedAt should not be zero")
			}
		})
	}
}

func TestNotificationEvent_IsPR(t *testing.T) {
	tests := []struct {
		name  string
		event NotificationEvent
		want  bool
	}{
		{
			name:  "PR opened is PR",
			event: NotificationEvent{Type: EventPROpened},
			want:  true,
		},
		{
			name:  "PR ready is PR",
			event: NotificationEvent{Type: EventPRReady},
			want:  true,
		},
		{
			name:  "Issue opened is not PR",
			event: NotificationEvent{Type: EventIssueOpened},
			want:  false,
		},
		{
			name:  "Comment added is not PR",
			event: NotificationEvent{Type: EventCommentAdded},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.IsPR(); got != tt.want {
				t.Errorf("IsPR() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotificationEvent_GitHubURL(t *testing.T) {
	event := NotificationEvent{
		RepoOwner:   "luminor-project",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Type:        EventIssueOpened,
	}

	want := "https://github.com/luminor-project/test-repo/issues/42"
	if got := event.GitHubURL(); got != want {
		t.Errorf("GitHubURL() = %v, want %v", got, want)
	}

	// Test PR URL
	event.Type = EventPROpened
	want = "https://github.com/luminor-project/test-repo/pull/42"
	if got := event.GitHubURL(); got != want {
		t.Errorf("GitHubURL() for PR = %v, want %v", got, want)
	}
}

func TestNotificationEvent_CommentURL(t *testing.T) {
	event := NotificationEvent{
		RepoOwner:   "luminor-project",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Type:        EventCommentAdded,
		CommentID:   123456,
	}

	want := "https://github.com/luminor-project/test-repo/issues/42#issuecomment-123456"
	if got := event.CommentURL(); got != want {
		t.Errorf("CommentURL() = %v, want %v", got, want)
	}
}
