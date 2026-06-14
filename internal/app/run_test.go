package app

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunLocalLogWithOpenAI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output_text":"{\"summary\":\"build failed\",\"likely_cause\":\"missing file\",\"retryable\":\"no\",\"failure_type\":\"unknown\",\"confidence\":0.7,\"next_actions\":[\"restore file\"],\"evidence\":[\"not found\"],\"owner_hint\":\"app\"}"}`))
	}))
	defer server.Close()

	logFile := filepath.Join(t.TempDir(), "failure.log")
	if err := os.WriteFile(logFile, []byte("password=secret\nerror: file not found"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"OPENAI_API_KEY": "test", "FLAKEHOUND_OPENAI_BASE_URL": server.URL}
	getenv := func(key string) string { return env[key] }
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"sniff", "log", "--log-file", logFile, "--provider", "openai", "--model", "test"}, &stdout, &stderr, getenv, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "build failed") || !strings.Contains(stdout.String(), "restore file") {
		t.Fatalf("unexpected output:\n%s", stdout.String())
	}
}

func TestRunGitHubDoesNotFailWhenPRCommentIsForbidden(t *testing.T) {
	logsArchive := testLogArchive(t, "Error: vet: undefined identifier")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/actions/runs/42":
			_, _ = w.Write([]byte(`{"id":42,"name":"CI","html_url":"https://example.test/runs/42","pull_requests":[{"number":7}]}`))
		case "/repos/owner/repo/actions/runs/42/jobs":
			_, _ = w.Write([]byte(`{"jobs":[{"name":"vet","conclusion":"failure"}]}`))
		case "/repos/owner/repo/actions/runs/42/logs":
			_, _ = w.Write(logsArchive)
		case "/responses":
			_, _ = w.Write([]byte(`{"output_text":"{\"summary\":\"vet failed\",\"likely_cause\":\"undefined identifier\",\"retryable\":\"no\",\"failure_type\":\"test_failure\",\"confidence\":0.9,\"next_actions\":[\"fix the identifier\"],\"evidence\":[\"Error: undefined identifier\"],\"owner_hint\":\"app\"}"}`))
		case "/repos/owner/repo/issues/7/comments":
			http.Error(w, `{"message":"Resource not accessible by integration"}`, http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	summaryPath := filepath.Join(t.TempDir(), "summary.md")
	env := map[string]string{
		"GITHUB_TOKEN":               "github-token",
		"OPENAI_API_KEY":             "openai-key",
		"FLAKEHOUND_GITHUB_API_URL":  server.URL,
		"FLAKEHOUND_OPENAI_BASE_URL": server.URL,
		"GITHUB_STEP_SUMMARY":        summaryPath,
	}
	getenv := func(key string) string { return env[key] }
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{
		"sniff", "github", "--repo", "owner/repo", "--run-id", "42",
		"--provider", "openai", "--model", "test", "--comment",
	}, &stdout, &stderr, getenv, "test")
	if err != nil {
		t.Fatalf("comment failure should not fail diagnosis: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning: diagnosis was generated, but the PR comment could not be posted") {
		t.Fatalf("expected comment warning, got: %s", stderr.String())
	}
	summary, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(summary), "vet failed") {
		t.Fatalf("diagnosis missing from summary: %s", summary)
	}
}

func testLogArchive(t *testing.T, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create("vet/1_vet.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
