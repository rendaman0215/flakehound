package render

import (
	"strings"
	"testing"

	"github.com/rendaman0215/flakehound/internal/diagnosis"
)

func TestMarkdown(t *testing.T) {
	got := Markdown(&diagnosis.Diagnosis{
		Summary: "Build failed", Retryable: "no", FailureType: "permission_error", Confidence: 0.82,
		LikelyCause: "Missing permission", NextActions: []string{"Ask platform"}, Evidence: []string{"- `Permission\n denied`"}, OwnerHint: "platform",
	})
	for _, want := range []string{Marker, "Flakehound Diagnosis", "**Retryable:** No", "0.82", "1. Ask platform", "- `'Permission denied'`"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Markdown missing %q:\n%s", want, got)
		}
	}
}

func TestMarkdownSanitizesModelControlledFields(t *testing.T) {
	got := Markdown(&diagnosis.Diagnosis{
		Summary:     "<img src=x onerror=alert(1)> [click](https://evil.example) @team\n# injected",
		LikelyCause: "![image](https://evil.example/image.png)",
		Retryable:   "yes](https://evil.example)",
		FailureType: "<script>alert(1)</script>",
		OwnerHint:   "@security",
		NextActions: []string{"- [Run this](https://evil.example)\n## injected"},
		Evidence:    []string{"failure `code` @team\n- injected"},
	})

	for _, unsafe := range []string{
		"<img", "<script", "](", "![", "https://", "@team", "@security", "\n# injected", "\n## injected",
	} {
		if strings.Contains(got, unsafe) {
			t.Fatalf("Markdown contains unsafe model output %q:\n%s", unsafe, got)
		}
	}
	for _, want := range []string{"&lt;img", "\\[click\\]", "@\u200bteam", "failure 'code' @\u200bteam"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Markdown missing sanitized content %q:\n%s", want, got)
		}
	}
}
