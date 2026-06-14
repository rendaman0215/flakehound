package render

import (
	"strings"
	"testing"

	"github.com/rendaman0215/flakehound/internal/diagnosis"
)

func TestMarkdown(t *testing.T) {
	got := Markdown(&diagnosis.Diagnosis{
		Summary: "Build failed", Retryable: "no", FailureType: "permission_error", Confidence: 0.82,
		LikelyCause: "Missing permission", NextActions: []string{"Ask platform"}, Evidence: []string{"Permission denied"}, OwnerHint: "platform",
	})
	for _, want := range []string{Marker, "Flakehound Diagnosis", "**Retryable:** No", "0.82", "1. Ask platform", "`Permission denied`"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Markdown missing %q:\n%s", want, got)
		}
	}
}
