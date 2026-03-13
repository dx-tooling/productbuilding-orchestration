package domain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
