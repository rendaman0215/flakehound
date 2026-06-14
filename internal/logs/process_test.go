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
