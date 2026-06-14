package diagnosis

import "testing"

func TestParseValidJSON(t *testing.T) {
	raw := `{"summary":"tests failed","likely_cause":"assertion","retryable":"NO","failure_type":"test_failure","confidence":1.4,"next_actions":[" inspect test "],"evidence":["FAIL TestThing"],"owner_hint":"app"}`
	d := Parse(raw)
	if d.Summary != "tests failed" || d.Retryable != "no" || d.Confidence != 1 || d.NextActions[0] != "inspect test" {
		t.Fatalf("unexpected diagnosis: %+v", d)
	}
}

func TestParseFallback(t *testing.T) {
	d := Parse("not json")
	if d.Summary != "not json" || d.Confidence != 0.3 || d.FailureType != "unknown" || d.Retryable != "unknown" {
		t.Fatalf("unexpected fallback: %+v", d)
	}
}

func TestParseCodeFence(t *testing.T) {
	d := Parse("```json\n{\"summary\":\"ok\",\"confidence\":0.8}\n```")
	if d.Summary != "ok" || d.Confidence != 0.8 {
		t.Fatalf("unexpected diagnosis: %+v", d)
	}
}
