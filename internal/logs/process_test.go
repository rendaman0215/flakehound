package logs

import (
	"strings"
	"testing"
)

func TestSanitizeSecrets(t *testing.T) {
	raw := "token=ghp_abcdefghijklmnopqrstuvwxyz123456\nAuthorization: Bearer abcdefghijklmnopqrstuvwxyz\npassword=hunter2\n-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----"
	got := Sanitize(raw)
	for _, secret := range []string{"ghp_", "abcdefghijklmnopqrstuvwxyz", "hunter2", "\nsecret\n"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret %q remained in %q", secret, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") || !strings.Contains(got, "[REDACTED PRIVATE KEY]") {
		t.Fatalf("expected redaction markers in %q", got)
	}
}

func TestSanitizeJWTAndSensitiveAssignments(t *testing.T) {
	raw := strings.Join([]string{
		"jwt=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature1234",
		"TOKEN=generic-token-value",
		"GITHUB_TOKEN=plain-but-sensitive",
		"export SERVICE_API_KEY=another-secret",
		"service_secret: 'quoted-secret'",
		"The word token appears in ordinary prose.",
		"monkey=banana",
	}, "\n")

	got := Sanitize(raw)
	for _, secret := range []string{"eyJhbGci", "generic-token-value", "plain-but-sensitive", "another-secret", "quoted-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret %q remained in %q", secret, got)
		}
	}
	for _, normal := range []string{"The word token appears in ordinary prose.", "monkey=banana"} {
		if !strings.Contains(got, normal) {
			t.Fatalf("normal text %q was over-redacted in %q", normal, got)
		}
	}
}

func TestProcessPrioritizesErrorsAndTail(t *testing.T) {
	var raw strings.Builder
	for i := 0; i < 300; i++ {
		if i == 40 {
			raw.WriteString("fatal: permission denied\n")
		} else {
			raw.WriteString("ordinary build output that is intentionally repetitive\n")
		}
	}
	raw.WriteString("final cleanup line\n")
	got := Process(raw.String(), 1500)
	if !strings.Contains(got, "permission denied") || !strings.Contains(got, "final cleanup line") {
		t.Fatalf("important lines missing from compressed log: %q", got)
	}
	if len(got) > 1500 {
		t.Fatalf("compressed log length = %d, want <= 1500", len(got))
	}
}

func TestProcessReservesTailBudgetAfterManyErrors(t *testing.T) {
	var raw strings.Builder
	for i := 0; i < 200; i++ {
		raw.WriteString("error: repeated failure consumes important-line budget\n")
	}
	raw.WriteString("tail-marker: workflow cleanup completed\n")

	got := Process(raw.String(), 600)
	if !strings.Contains(got, "tail-marker: workflow cleanup completed") {
		t.Fatalf("reserved tail missing from compressed log: %q", got)
	}
	if len(got) > 600 {
		t.Fatalf("compressed log length = %d, want <= 600", len(got))
	}
}

func TestProcessPrioritizesMatchingFailedJobSection(t *testing.T) {
	raw := strings.Join([]string{
		"===== lint/1_lint.txt =====",
		"setup lint",
		"error: lint infrastructure warning",
		"lint cleanup",
		strings.Repeat("ordinary lint output ", 20),
		"===== test-linux/2_test.txt =====",
		"setup tests",
		"TestCheckout exited with status 1",
		"test cleanup",
		strings.Repeat("ordinary test output ", 20),
		"===== cleanup/3_cleanup.txt =====",
		"final archive upload complete",
	}, "\n")

	got := Process(raw, 700, "test-linux")
	failedIndex := strings.Index(got, "TestCheckout exited with status 1")
	unrelatedIndex := strings.Index(got, "lint infrastructure warning")
	if failedIndex < 0 {
		t.Fatalf("failed job evidence missing from compressed log: %q", got)
	}
	if unrelatedIndex >= 0 && failedIndex > unrelatedIndex {
		t.Fatalf("failed job evidence was not prioritized: %q", got)
	}
	if !strings.Contains(got, "final archive upload complete") {
		t.Fatalf("log tail missing from compressed log: %q", got)
	}
}
