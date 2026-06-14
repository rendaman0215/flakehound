package diagnosis

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type Diagnoser interface {
	Diagnose(ctx context.Context, input DiagnosisInput) (*Diagnosis, error)
}

type DiagnosisInput struct {
	Repo       string
	RunID      int64
	RunURL     string
	Workflow   string
	FailedJobs []string
	Log        string
}

type Diagnosis struct {
	Summary     string   `json:"summary"`
	LikelyCause string   `json:"likely_cause"`
	Retryable   string   `json:"retryable"`
	FailureType string   `json:"failure_type"`
	Confidence  float64  `json:"confidence"`
	NextActions []string `json:"next_actions"`
	Evidence    []string `json:"evidence"`
	OwnerHint   string   `json:"owner_hint"`
}

var (
	validRetryable = set("yes", "no", "unknown")
	validFailures  = set("test_failure", "dependency_failure", "permission_error", "infra_error", "flaky_suspected", "unknown")
	validOwners    = set("app", "platform", "security", "unknown")
)

func Parse(raw string) *Diagnosis {
	cleaned := stripCodeFence(strings.TrimSpace(raw))
	var d Diagnosis
	if cleaned == "" || json.Unmarshal([]byte(cleaned), &d) != nil {
		return fallback(raw)
	}

	d.Summary = strings.TrimSpace(d.Summary)
	d.LikelyCause = strings.TrimSpace(d.LikelyCause)
	d.Retryable = normalizeEnum(d.Retryable, validRetryable)
	d.FailureType = normalizeEnum(d.FailureType, validFailures)
	d.OwnerHint = normalizeEnum(d.OwnerHint, validOwners)
	if math.IsNaN(d.Confidence) || math.IsInf(d.Confidence, 0) {
		d.Confidence = 0.3
	}
	d.Confidence = math.Max(0, math.Min(1, d.Confidence))
	d.NextActions = cleanList(d.NextActions)
	d.Evidence = cleanList(d.Evidence)
	if d.Summary == "" {
		d.Summary = "The model returned a diagnosis without a summary."
	}
	return &d
}

func Prompt(input DiagnosisInput) string {
	return fmt.Sprintf(`Diagnose this CI failure for a software developer.

Return only a JSON object with exactly these fields:
{
  "summary": "short explanation",
  "likely_cause": "what likely happened",
  "retryable": "yes | no | unknown",
  "failure_type": "test_failure | dependency_failure | permission_error | infra_error | flaky_suspected | unknown",
  "confidence": 0.0,
  "next_actions": ["action 1"],
  "evidence": ["important log line"],
  "owner_hint": "app | platform | security | unknown"
}

Base claims on the supplied evidence. Do not invent logs, permissions, owners, or infrastructure details. Use "unknown" when the evidence is insufficient. Keep evidence entries short and copied or closely paraphrased from the log.

Repository: %s
Workflow: %s
Run ID: %d
Run URL: %s
Failed jobs: %s

Sanitized log evidence:
---
%s
---`, valueOrUnknown(input.Repo), valueOrUnknown(input.Workflow), input.RunID, valueOrUnknown(input.RunURL), listOrUnknown(input.FailedJobs), input.Log)
}

func JSONSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"summary":      map[string]any{"type": "string"},
			"likely_cause": map[string]any{"type": "string"},
			"retryable":    map[string]any{"type": "string", "enum": []string{"yes", "no", "unknown"}},
			"failure_type": map[string]any{"type": "string", "enum": []string{"test_failure", "dependency_failure", "permission_error", "infra_error", "flaky_suspected", "unknown"}},
			"confidence":   map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"next_actions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"evidence":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"owner_hint":   map[string]any{"type": "string", "enum": []string{"app", "platform", "security", "unknown"}},
		},
		"required": []string{"summary", "likely_cause", "retryable", "failure_type", "confidence", "next_actions", "evidence", "owner_hint"},
	}
}

func fallback(raw string) *Diagnosis {
	summary := strings.TrimSpace(raw)
	if summary == "" {
		summary = "The model returned an empty response."
	}
	return &Diagnosis{
		Summary:     summary,
		Retryable:   "unknown",
		FailureType: "unknown",
		Confidence:  0.3,
		OwnerHint:   "unknown",
	}
}

func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 3 || !strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		return s
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
}

func normalizeEnum(value string, allowed map[string]struct{}) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if _, ok := allowed[value]; ok {
		return value
	}
	return "unknown"
}

func cleanList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func set(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func valueOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func listOrUnknown(values []string) string {
	if len(values) == 0 {
		return "unknown"
	}
	return strings.Join(values, ", ")
}
