package github

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkflowRunLogsAndComment(t *testing.T) {
	archive := zipArchive(t, map[string]string{"build/1_test.txt": "FAIL TestThing", "ignored.json": "{}"})
	var commentBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing auth header")
		}
		switch r.URL.Path {
		case "/repos/owner/repo/actions/runs/42":
			_, _ = w.Write([]byte(`{"id":42,"name":"CI","html_url":"https://example/run/42","pull_requests":[{"number":7}]}`))
		case "/repos/owner/repo/actions/runs/42/jobs":
			_, _ = w.Write([]byte(`{"jobs":[{"name":"test","conclusion":"failure"},{"name":"lint","conclusion":"success"}]}`))
		case "/repos/owner/repo/actions/runs/42/logs":
			_, _ = w.Write(archive)
		case "/repos/owner/repo/issues/7/comments":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			commentBody = body["body"]
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewWithOptions("token", server.URL, server.Client())
	run, err := client.GetRun(context.Background(), "owner/repo", 42)
	if err != nil || run.Name != "CI" || run.PullRequests[0].Number != 7 {
		t.Fatalf("unexpected run=%+v err=%v", run, err)
	}
	jobs, err := client.FailedJobs(context.Background(), "owner/repo", 42)
	if err != nil || len(jobs) != 1 || jobs[0] != "test" {
		t.Fatalf("unexpected jobs=%v err=%v", jobs, err)
	}
	logs, err := client.DownloadLogs(context.Background(), "owner/repo", 42)
	if err != nil || !strings.Contains(logs, "FAIL TestThing") {
		t.Fatalf("unexpected logs=%q err=%v", logs, err)
	}
	if err := client.CreatePRComment(context.Background(), "owner/repo", 7, "diagnosis"); err != nil {
		t.Fatal(err)
	}
	if commentBody != "diagnosis" {
		t.Fatalf("comment body = %q", commentBody)
	}
}

func zipArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
