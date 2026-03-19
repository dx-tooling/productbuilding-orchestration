package domain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Client interacts with the GitHub API for tarball downloads and PR comments.
type Client struct {
	httpClient *http.Client
	baseURL    string // optional, for testing; defaults to "https://api.github.com"
}

func NewClient() *Client {
	return &Client{httpClient: &http.Client{}}
}

// DownloadSource downloads a repo tarball from GitHub and extracts it to destDir.
func (c *Client) DownloadSource(ctx context.Context, owner, repo, sha, pat, destDir string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/tarball/%s", owner, repo, sha)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")

	slog.Info("downloading tarball", "repo", owner+"/"+repo, "sha", sha[:8])

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download tarball: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("download tarball: status %d: %s", resp.StatusCode, body)
	}

	if err := extractTarGz(resp.Body, destDir); err != nil {
		return "", fmt.Errorf("extract tarball: %w", err)
	}

	slog.Info("tarball extracted", "dest", destDir)
	return destDir, nil
}

func extractTarGz(r io.Reader, destDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar reader: %w", err)
		}

		// GitHub tarballs have a root dir like "owner-repo-sha1234/". Strip it.
		parts := strings.SplitN(header.Name, "/", 2)
		if len(parts) < 2 || parts[1] == "" {
			continue
		}
		relPath := parts[1]

		target := filepath.Join(destDir, relPath)

		// Prevent zip slip
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid tar path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("mkdir parent: %w", err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("create file: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write file: %w", err)
			}
			f.Close()
		}
	}

	return nil
}

type commentPayload struct {
	Body string `json:"body"`
}

type commentResponse struct {
	ID int64 `json:"id"`
}

// CreateComment posts a new comment on a PR and returns the comment ID.
func (c *Client) CreateComment(ctx context.Context, owner, repo string, prNumber int, body, pat string) (int64, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", owner, repo, prNumber)

	payload, _ := json.Marshal(commentPayload{Body: body})

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("create comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("create comment: status %d: %s", resp.StatusCode, respBody)
	}

	var result commentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("parse comment response: %w", err)
	}

	return result.ID, nil
}

// UpdateComment edits an existing PR comment.
func (c *Client) UpdateComment(ctx context.Context, owner, repo string, commentID int64, body, pat string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/comments/%d", owner, repo, commentID)

	payload, _ := json.Marshal(commentPayload{Body: body})

	req, err := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("update comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update comment: status %d: %s", resp.StatusCode, respBody)
	}

	return nil
}

// apiURL returns the base GitHub API URL, using the configured baseURL if set.
func (c *Client) apiURL() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return "https://api.github.com"
}

type createIssueRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type createIssueResponse struct {
	Number int `json:"number"`
}

// CreateIssue creates a new GitHub issue and returns its number.
func (c *Client) CreateIssue(ctx context.Context, owner, repo, title, body, pat string) (int, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues", c.apiURL(), owner, repo)

	payload, _ := json.Marshal(createIssueRequest{Title: title, Body: body})

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("create issue: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("create issue: status %d: %s", resp.StatusCode, respBody)
	}

	var result createIssueResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("parse issue response: %w", err)
	}

	return result.Number, nil
}

// IssueSearchResult represents a single issue in search results.
type IssueSearchResult struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	URL    string `json:"html_url"`
}

type searchIssuesResponse struct {
	Items []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		URL    string `json:"html_url"`
	} `json:"items"`
}

// SearchIssues searches for issues in a repository.
func (c *Client) SearchIssues(ctx context.Context, owner, repo, query, pat string) ([]IssueSearchResult, error) {
	q := fmt.Sprintf("repo:%s/%s is:issue %s", owner, repo, query)
	url := fmt.Sprintf("%s/search/issues?q=%s&per_page=10", c.apiURL(), strings.ReplaceAll(q, " ", "+"))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search issues: status %d: %s", resp.StatusCode, respBody)
	}

	var result searchIssuesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse search response: %w", err)
	}

	issues := make([]IssueSearchResult, len(result.Items))
	for i, item := range result.Items {
		issues[i] = IssueSearchResult{
			Number: item.Number,
			Title:  item.Title,
			State:  item.State,
			URL:    item.URL,
		}
	}

	return issues, nil
}

// IssueDetail represents full details of a GitHub issue.
type IssueDetail struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	State  string   `json:"state"`
	URL    string   `json:"html_url"`
	User   string   `json:"user_login"`
	Labels []string `json:"labels"`
}

type issueDetailResponse struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	URL    string `json:"html_url"`
	User   struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func issueDetailFromResponse(r issueDetailResponse) IssueDetail {
	labels := make([]string, len(r.Labels))
	for i, l := range r.Labels {
		labels[i] = l.Name
	}
	return IssueDetail{
		Number: r.Number,
		Title:  r.Title,
		Body:   r.Body,
		State:  r.State,
		URL:    r.URL,
		User:   r.User.Login,
		Labels: labels,
	}
}

// GetIssue retrieves details of a specific issue.
func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int, pat string) (*IssueDetail, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d", c.apiURL(), owner, repo, number)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get issue: status %d: %s", resp.StatusCode, respBody)
	}

	var result issueDetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse issue response: %w", err)
	}

	detail := issueDetailFromResponse(result)
	return &detail, nil
}

// ListIssues lists issues in a repository.
func (c *Client) ListIssues(ctx context.Context, owner, repo, state, pat string, limit int) ([]IssueDetail, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues?state=%s&per_page=%d", c.apiURL(), owner, repo, state, limit)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list issues: status %d: %s", resp.StatusCode, respBody)
	}

	var items []issueDetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("parse list response: %w", err)
	}

	issues := make([]IssueDetail, len(items))
	for i, item := range items {
		issues[i] = issueDetailFromResponse(item)
	}

	return issues, nil
}

// GetPRDiff retrieves the diff of a pull request as plain text.
func (c *Client) GetPRDiff(ctx context.Context, owner, repo string, prNumber int, pat string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.apiURL(), owner, repo, prNumber)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github.diff")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("get pr diff: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("get pr diff: status %d: %s", resp.StatusCode, respBody)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read diff: %w", err)
	}

	return string(body), nil
}

type updateIssueStateRequest struct {
	State string `json:"state"`
}

// CloseIssue closes a GitHub issue.
func (c *Client) CloseIssue(ctx context.Context, owner, repo string, number int, pat string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d", c.apiURL(), owner, repo, number)

	payload, _ := json.Marshal(updateIssueStateRequest{State: "closed"})

	req, err := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("close issue: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("close issue: status %d: %s", resp.StatusCode, respBody)
	}

	return nil
}

type updatePRStateRequest struct {
	State string `json:"state"`
}

// ClosePR closes a GitHub pull request.
func (c *Client) ClosePR(ctx context.Context, owner, repo string, prNumber int, pat string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.apiURL(), owner, repo, prNumber)

	payload, _ := json.Marshal(updatePRStateRequest{State: "closed"})

	req, err := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("close pr: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("close pr: status %d: %s", resp.StatusCode, respBody)
	}

	return nil
}

// CodeSearchResult represents a file matching a code search.
type CodeSearchResult struct {
	Path        string   `json:"path"`
	HTMLURL     string   `json:"html_url"`
	TextMatches []string `json:"text_matches"`
}

type searchCodeResponse struct {
	Items []searchCodeItem `json:"items"`
}

type searchCodeItem struct {
	Path        string `json:"path"`
	HTMLURL     string `json:"html_url"`
	TextMatches []struct {
		Fragment string `json:"fragment"`
	} `json:"text_matches"`
}

// SearchCode searches for code in a repository using GitHub code search.
func (c *Client) SearchCode(ctx context.Context, owner, repo, query, pat string) ([]CodeSearchResult, error) {
	q := fmt.Sprintf("repo:%s/%s %s", owner, repo, query)
	u := fmt.Sprintf("%s/search/code?q=%s&per_page=10", c.apiURL(), strings.ReplaceAll(q, " ", "+"))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github.text-match+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search code: status %d: %s", resp.StatusCode, respBody)
	}

	var result searchCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse search response: %w", err)
	}

	items := make([]CodeSearchResult, len(result.Items))
	for i, item := range result.Items {
		fragments := make([]string, len(item.TextMatches))
		for j, m := range item.TextMatches {
			fragments[j] = m.Fragment
		}
		items[i] = CodeSearchResult{
			Path:        item.Path,
			HTMLURL:     item.HTMLURL,
			TextMatches: fragments,
		}
	}
	return items, nil
}

// FileContents represents the contents of a file or directory from the GitHub API.
type FileContents struct {
	Path    string     `json:"path"`
	Type    string     `json:"type"` // "file" or "dir"
	Size    int        `json:"size"`
	Content string     `json:"content,omitempty"`
	Entries []DirEntry `json:"entries,omitempty"`
}

// DirEntry represents a single entry in a directory listing.
type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // "file" or "dir"
	Size int    `json:"size"`
}

// GetFileContents retrieves the contents of a file or directory listing at any ref.
func (c *Client) GetFileContents(ctx context.Context, owner, repo, path, ref, pat string) (*FileContents, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.apiURL(), owner, repo, path)
	if ref != "" {
		u += "?ref=" + url.QueryEscape(ref)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get file contents: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get file contents: status %d: %s", resp.StatusCode, respBody)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Try as file (object with type "file")
	var fileResp struct {
		Type     string `json:"type"`
		Path     string `json:"path"`
		Size     int    `json:"size"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(body, &fileResp); err == nil && fileResp.Type == "file" {
		content := fileResp.Content
		if fileResp.Encoding == "base64" {
			cleaned := strings.ReplaceAll(content, "\n", "")
			decoded, err := base64.StdEncoding.DecodeString(cleaned)
			if err != nil {
				return nil, fmt.Errorf("decode base64 content: %w", err)
			}
			content = string(decoded)
		}
		return &FileContents{
			Path:    fileResp.Path,
			Type:    "file",
			Size:    fileResp.Size,
			Content: content,
		}, nil
	}

	// Try as directory (array)
	var dirResp []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
		Size int    `json:"size"`
	}
	if err := json.Unmarshal(body, &dirResp); err == nil && len(dirResp) > 0 {
		entries := make([]DirEntry, len(dirResp))
		for i, e := range dirResp {
			entries[i] = DirEntry{Name: e.Name, Path: e.Path, Type: e.Type, Size: e.Size}
		}
		return &FileContents{
			Path:    path,
			Type:    "dir",
			Entries: entries,
		}, nil
	}

	return nil, fmt.Errorf("unexpected response format for %s", path)
}

// WorkflowRun represents a GitHub Actions workflow run.
type WorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	HeadBranch string `json:"head_branch"`
	Event      string `json:"event"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type listWorkflowRunsResponse struct {
	WorkflowRuns []WorkflowRun `json:"workflow_runs"`
}

// ListWorkflowRuns lists recent workflow runs, optionally filtered by branch.
func (c *Client) ListWorkflowRuns(ctx context.Context, owner, repo, branch, pat string, limit int) ([]WorkflowRun, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/actions/runs?per_page=%d", c.apiURL(), owner, repo, limit)
	if branch != "" {
		u += "&branch=" + url.QueryEscape(branch)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list workflow runs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list workflow runs: status %d: %s", resp.StatusCode, respBody)
	}

	var result listWorkflowRunsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse workflow runs response: %w", err)
	}

	return result.WorkflowRuns, nil
}

// WorkflowRunJob represents a job within a workflow run.
type WorkflowRunJob struct {
	ID         int64             `json:"id"`
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	Conclusion string            `json:"conclusion"`
	HTMLURL    string            `json:"html_url"`
	Steps      []WorkflowRunStep `json:"steps"`
}

// WorkflowRunStep represents a step within a job.
type WorkflowRunStep struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Number     int    `json:"number"`
}

type listWorkflowRunJobsResponse struct {
	Jobs []WorkflowRunJob `json:"jobs"`
}

// ListWorkflowRunJobs lists jobs for a specific workflow run.
func (c *Client) ListWorkflowRunJobs(ctx context.Context, owner, repo string, runID int64, pat string) ([]WorkflowRunJob, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/jobs", c.apiURL(), owner, repo, runID)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list workflow run jobs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list workflow run jobs: status %d: %s", resp.StatusCode, respBody)
	}

	var result listWorkflowRunJobsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse workflow run jobs response: %w", err)
	}

	return result.Jobs, nil
}

// GetJobLogs downloads the logs for a specific GitHub Actions job.
func (c *Client) GetJobLogs(ctx context.Context, owner, repo string, jobID int64, pat string) (string, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/actions/jobs/%d/logs", c.apiURL(), owner, repo, jobID)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("get job logs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("get job logs: status %d: %s", resp.StatusCode, respBody)
	}

	// Cap at 1MB to protect against huge logs
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read job logs: %w", err)
	}

	return string(body), nil
}

// DeleteComment removes a PR comment.
func (c *Client) DeleteComment(ctx context.Context, owner, repo string, commentID int64, pat string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/comments/%d", owner, repo, commentID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete comment: status %d: %s", resp.StatusCode, respBody)
	}

	return nil
}

// listCommentResponse represents a GitHub issue comment with full details
type listCommentResponse struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
}

// orchestratorMarker is the unique HTML comment that identifies our bot comments
const orchestratorMarker = "<!-- productbuilding-orchestrator -->"

// DeleteAllBotComments removes all our bot comments from a PR (identified by unique marker)
func (c *Client) DeleteAllBotComments(ctx context.Context, owner, repo string, prNumber int, pat string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", owner, repo, prNumber)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("list comments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("list comments: status %d: %s", resp.StatusCode, respBody)
	}

	var comments []listCommentResponse
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		return fmt.Errorf("decode comments: %w", err)
	}

	// Delete only comments with our unique marker
	for _, comment := range comments {
		if strings.Contains(comment.Body, orchestratorMarker) {
			if err := c.DeleteComment(ctx, owner, repo, comment.ID, pat); err != nil {
				slog.Warn("failed to delete old orchestrator comment", "comment_id", comment.ID, "error", err)
			}
		}
	}

	return nil
}
