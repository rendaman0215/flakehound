package openai

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
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected request: %s, auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "test-model" || body["text"] == nil {
			t.Fatalf("unexpected body: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"content":[{"type":"output_text","text":"{\"summary\":\"dependency failed\",\"likely_cause\":\"registry timeout\",\"retryable\":\"yes\",\"failure_type\":\"dependency_failure\",\"confidence\":0.8,\"next_actions\":[\"retry\"],\"evidence\":[\"timeout\"],\"owner_hint\":\"platform\"}"}]}]}`))
	}))
	defer server.Close()

	client := NewWithOptions("test-key", "test-model", server.URL, server.Client())
	got, err := client.Diagnose(context.Background(), diagnosis.DiagnosisInput{Log: "timeout"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "dependency failed" || got.Retryable != "yes" {
		t.Fatalf("unexpected diagnosis: %+v", got)
	}
}

func TestMissingAPIKey(t *testing.T) {
	_, err := New("", "").Diagnose(context.Background(), diagnosis.DiagnosisInput{})
	if err == nil || err.Error() != "OPENAI_API_KEY is required for provider openai" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiagnoseRetriesTransientFailure(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "busy", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"output_text":"{\"summary\":\"recovered\"}"}`))
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
		http.Error(w, "invalid request", http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewWithOptions("test-key", "test-model", server.URL, server.Client())
	_, err := client.Diagnose(context.Background(), diagnosis.DiagnosisInput{})
	if err == nil || !strings.Contains(err.Error(), "400 Bad Request") || attempts != 1 {
		t.Fatalf("attempts=%d error=%v", attempts, err)
	}
}
