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

func TestFailedJobsPaginates(t *testing.T) {
	var requestedPages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPages = append(requestedPages, r.URL.Query().Get("page"))
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("Link", `<http://example.test/jobs?page=2>; rel="next"`)
			_, _ = w.Write([]byte(`{"total_count":2,"jobs":[{"name":"linux","conclusion":"failure"}]}`))
		case "2":
			_, _ = w.Write([]byte(`{"total_count":2,"jobs":[{"name":"windows","conclusion":"timed_out"}]}`))
		default:
			http.Error(w, "unexpected page", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := NewWithOptions("token", server.URL, server.Client())
	jobs, err := client.FailedJobs(context.Background(), "owner/repo", 42)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(jobs, ",") != "linux,windows" {
		t.Fatalf("failed jobs = %v, want both pages", jobs)
	}
	if strings.Join(requestedPages, ",") != "1,2" {
		t.Fatalf("requested pages = %v, want [1 2]", requestedPages)
	}
}

func TestUnpackLogsEnforcesLimits(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		limits  logArchiveLimits
		wantErr string
	}{
		{
			name:    "per file bytes",
			files:   map[string]string{"large.txt": "123456"},
			limits:  logArchiveLimits{maxFileBytes: 5, maxTotalBytes: 100, maxFiles: 10},
			wantErr: "log file large.txt exceeds 5 bytes",
		},
		{
			name:    "total bytes",
			files:   map[string]string{"one.txt": "1234", "two.txt": "5678"},
			limits:  logArchiveLimits{maxFileBytes: 10, maxTotalBytes: 7, maxFiles: 10},
			wantErr: "workflow logs exceed 7 total bytes",
		},
		{
			name:    "file count",
			files:   map[string]string{"one.txt": "1", "two.txt": "2", "metadata.json": "{}"},
			limits:  logArchiveLimits{maxFileBytes: 10, maxTotalBytes: 100, maxFiles: 2},
			wantErr: "archive contains 3 files, limit is 2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := unpackLogsWithLimits(zipArchive(t, test.files), test.limits)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, test.wantErr)
			}
		})
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
