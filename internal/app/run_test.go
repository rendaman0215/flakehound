package app

import (
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
