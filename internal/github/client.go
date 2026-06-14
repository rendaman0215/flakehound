package github

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	defaultBaseURL      = "https://api.github.com"
	maxArchiveSize      = 64 << 20
	maxLogFileSize      = 16 << 20
	maxTotalLogSize     = 64 << 20
	maxArchiveFileCount = 1_000
)

type logArchiveLimits struct {
	maxFileBytes  int64
	maxTotalBytes int64
	maxFiles      int
}

type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

type WorkflowRun struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	HTMLURL      string `json:"html_url"`
	PullRequests []struct {
		Number int `json:"number"`
	} `json:"pull_requests"`
}

type Job struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
}

func New(token string) *Client {
	return NewWithOptions(token, defaultBaseURL, &http.Client{Timeout: 90 * time.Second})
}

func NewWithOptions(token, baseURL string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}
	return &Client{token: token, baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}
}

func (c *Client) GetRun(ctx context.Context, repo string, runID int64) (*WorkflowRun, error) {
	var run WorkflowRun
	if err := c.getJSON(ctx, repoPath(repo, fmt.Sprintf("actions/runs/%d", runID)), &run); err != nil {
		return nil, fmt.Errorf("get workflow run: %w", err)
	}
	return &run, nil
}

func (c *Client) FailedJobs(ctx context.Context, repo string, runID int64) ([]string, error) {
	var names []string
	for page := 1; ; page++ {
		var response struct {
			TotalCount int   `json:"total_count"`
			Jobs       []Job `json:"jobs"`
		}
		endpoint := repoPath(repo, fmt.Sprintf("actions/runs/%d/jobs?filter=latest&per_page=100&page=%d", runID, page))
		hasNext, err := c.getJSONPage(ctx, endpoint, &response)
		if err != nil {
			return nil, fmt.Errorf("list workflow jobs: %w", err)
		}
		for _, job := range response.Jobs {
			if job.Conclusion == "failure" || job.Conclusion == "timed_out" || job.Conclusion == "cancelled" || job.Conclusion == "action_required" {
				names = append(names, job.Name)
			}
		}
		if !hasNext && len(response.Jobs) < 100 && response.TotalCount <= page*100 {
			break
		}
	}
	return names, nil
}

func (c *Client) DownloadLogs(ctx context.Context, repo string, runID int64) (string, error) {
	req, err := c.request(ctx, http.MethodGet, repoPath(repo, fmt.Sprintf("actions/runs/%d/logs", runID)), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download workflow logs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", responseError(resp)
	}
	archive, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveSize+1))
	if err != nil {
		return "", fmt.Errorf("read workflow logs archive: %w", err)
	}
	if len(archive) > maxArchiveSize {
		return "", fmt.Errorf("workflow logs archive exceeds %d MiB", maxArchiveSize>>20)
	}
	return unpackLogs(archive)
}

func (c *Client) CreatePRComment(ctx context.Context, repo string, prNumber int, body string) error {
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return err
	}
	req, err := c.request(ctx, http.MethodPost, repoPath(repo, fmt.Sprintf("issues/%d/comments", prNumber)), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("create PR comment: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("create PR comment: %w", responseError(resp))
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, endpoint string, target any) error {
	_, err := c.getJSONPage(ctx, endpoint, target)
	return err
}

func (c *Client) getJSONPage(ctx context.Context, endpoint string, target any) (bool, error) {
	req, err := c.request(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, responseError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return false, fmt.Errorf("decode GitHub response: %w", err)
	}
	return strings.Contains(resp.Header.Get("Link"), `rel="next"`), nil
}

func (c *Client) request(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	if strings.TrimSpace(c.token) == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN is required for GitHub workflow analysis")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return req, nil
}

func repoPath(repo, suffix string) string {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return "/repos/invalid/invalid/" + suffix
	}
	base := path.Join("/repos", url.PathEscape(parts[0]), url.PathEscape(parts[1]))
	if strings.Contains(suffix, "?") {
		pieces := strings.SplitN(suffix, "?", 2)
		return path.Join(base, pieces[0]) + "?" + pieces[1]
	}
	return path.Join(base, suffix)
}

func unpackLogs(archive []byte) (string, error) {
	return unpackLogsWithLimits(archive, logArchiveLimits{
		maxFileBytes:  maxLogFileSize,
		maxTotalBytes: maxTotalLogSize,
		maxFiles:      maxArchiveFileCount,
	})
}

func unpackLogsWithLimits(archive []byte, limits logArchiveLimits) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return "", fmt.Errorf("open workflow logs archive: %w", err)
	}
	if len(zr.File) > limits.maxFiles {
		return "", fmt.Errorf("workflow logs archive contains %d files, limit is %d", len(zr.File), limits.maxFiles)
	}
	var result strings.Builder
	var totalBytes int64
	for _, file := range zr.File {
		if file.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(file.Name), ".txt") {
			continue
		}
		if file.UncompressedSize64 > uint64(limits.maxFileBytes) {
			return "", fmt.Errorf("log file %s exceeds %d bytes", file.Name, limits.maxFileBytes)
		}
		remainingTotal := limits.maxTotalBytes - totalBytes
		if remainingTotal <= 0 {
			return "", fmt.Errorf("workflow logs exceed %d total bytes", limits.maxTotalBytes)
		}
		readLimit := min(limits.maxFileBytes, remainingTotal)
		r, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("open log file %s: %w", file.Name, err)
		}
		content, readErr := io.ReadAll(io.LimitReader(r, readLimit+1))
		closeErr := r.Close()
		if readErr != nil {
			return "", fmt.Errorf("read log file %s: %w", file.Name, readErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close log file %s: %w", file.Name, closeErr)
		}
		if int64(len(content)) > limits.maxFileBytes {
			return "", fmt.Errorf("log file %s exceeds %d bytes", file.Name, limits.maxFileBytes)
		}
		if int64(len(content)) > remainingTotal {
			return "", fmt.Errorf("workflow logs exceed %d total bytes", limits.maxTotalBytes)
		}
		totalBytes += int64(len(content))
		fmt.Fprintf(&result, "\n===== %s =====\n%s\n", file.Name, content)
	}
	if result.Len() == 0 {
		return "", fmt.Errorf("workflow logs archive contained no text logs")
	}
	return strings.TrimSpace(result.String()), nil
}

func responseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("GitHub API returned %s: %s", resp.Status, message)
}
