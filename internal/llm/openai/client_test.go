package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
