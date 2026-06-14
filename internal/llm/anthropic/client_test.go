package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rendaman0215/flakehound/internal/diagnosis"
)

func TestDiagnose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" || r.Header.Get("x-api-key") != "test-key" || r.Header.Get("anthropic-version") == "" {
			t.Fatalf("unexpected request: %s headers=%v", r.URL.Path, r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "test-model" || body["messages"] == nil {
			t.Fatalf("unexpected body: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{\"summary\":\"test failed\",\"likely_cause\":\"assertion\",\"retryable\":\"no\",\"failure_type\":\"test_failure\",\"confidence\":0.9,\"next_actions\":[\"fix test\"],\"evidence\":[\"FAIL\"],\"owner_hint\":\"app\"}"}]}`))
	}))
	defer server.Close()

	client := NewWithOptions("test-key", "test-model", server.URL, server.Client())
	got, err := client.Diagnose(context.Background(), diagnosis.DiagnosisInput{Log: "FAIL"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "test failed" || got.OwnerHint != "app" {
		t.Fatalf("unexpected diagnosis: %+v", got)
	}
}

func TestMissingAPIKey(t *testing.T) {
	_, err := New("", "").Diagnose(context.Background(), diagnosis.DiagnosisInput{})
	if err == nil || err.Error() != "ANTHROPIC_API_KEY is required for provider anthropic" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiagnoseRetriesTransientFailure(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{\"summary\":\"recovered\"}"}]}`))
	}))
	defer server.Close()

	client := NewWithOptions("test-key", "test-model", server.URL, server.Client())
	client.retry.Sleep = func(context.Context, time.Duration) error { return nil }
	got, err := client.Diagnose(context.Background(), diagnosis.DiagnosisInput{})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || got.Summary != "recovered" {
		t.Fatalf("attempts=%d diagnosis=%+v", attempts, got)
	}
}

func TestDiagnoseDoesNotRetryPermanentFailure(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		http.Error(w, "invalid request", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewWithOptions("test-key", "test-model", server.URL, server.Client())
	_, err := client.Diagnose(context.Background(), diagnosis.DiagnosisInput{})
	if err == nil || !strings.Contains(err.Error(), "401 Unauthorized") || attempts != 1 {
		t.Fatalf("attempts=%d error=%v", attempts, err)
	}
}
