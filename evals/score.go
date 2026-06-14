package evals

import (
	"fmt"
	"sort"
	"strings"
)

// Diagnosis is the subset of a captured model response used by the scorer.
type Diagnosis struct {
	FailureType string   `json:"failure_type"`
	Retryable   string   `json:"retryable"`
	OwnerHint   string   `json:"owner_hint"`
	Evidence    []string `json:"evidence"`
}

// CaseScore reports each independently scored component. Total is in [0, 1],
// with equal weight for failure type, retryability, owner, and evidence.
type CaseScore struct {
	CaseID              string   `json:"case_id"`
	FailureTypeMatch    bool     `json:"failure_type_match"`
	RetryableMatch      bool     `json:"retryable_match"`
	OwnerHintMatch      bool     `json:"owner_hint_match"`
	EvidenceMatched     int      `json:"evidence_matched"`
	EvidenceExpected    int      `json:"evidence_expected"`
	EvidenceMatchedList []string `json:"evidence_matched_substrings"`
	Total               float64  `json:"total"`
}

// DatasetScore aggregates case scores using an unweighted mean.
type DatasetScore struct {
	Cases []CaseScore `json:"cases"`
	Total float64     `json:"total"`
}

// ScoreCase deterministically compares one supplied diagnosis with a golden
// expectation. Enum comparison ignores surrounding whitespace and case.
func ScoreCase(testCase Case, diagnosis Diagnosis) CaseScore {
	score := CaseScore{
		CaseID:           testCase.ID,
		FailureTypeMatch: equalNormalized(testCase.Expectation.FailureType, diagnosis.FailureType),
		RetryableMatch:   equalNormalized(testCase.Expectation.Retryable, diagnosis.Retryable),
		OwnerHintMatch:   equalNormalized(testCase.Expectation.OwnerHint, diagnosis.OwnerHint),
		EvidenceExpected: len(testCase.Expectation.EvidenceSubstrings),
	}

	evidence := strings.ToLower(strings.Join(diagnosis.Evidence, "\n"))
	for _, expected := range testCase.Expectation.EvidenceSubstrings {
		if strings.Contains(evidence, strings.ToLower(strings.TrimSpace(expected))) {
			score.EvidenceMatched++
			score.EvidenceMatchedList = append(score.EvidenceMatchedList, expected)
		}
	}

	points := boolPoint(score.FailureTypeMatch) + boolPoint(score.RetryableMatch) + boolPoint(score.OwnerHintMatch)
	evidenceScore := 0.0
	if score.EvidenceExpected > 0 {
		evidenceScore = float64(score.EvidenceMatched) / float64(score.EvidenceExpected)
	}
	score.Total = (points + evidenceScore) / 4
	return score
}

// ScoreDataset scores one diagnosis per case. Missing and unknown case IDs are
// rejected so incomplete captured-output files cannot silently inflate scores.
func ScoreDataset(dataset Dataset, diagnoses map[string]Diagnosis) (DatasetScore, error) {
	if len(dataset.Cases) == 0 {
		return DatasetScore{}, fmt.Errorf("cannot score an empty dataset")
	}
	known := make(map[string]struct{}, len(dataset.Cases))
	result := DatasetScore{Cases: make([]CaseScore, 0, len(dataset.Cases))}
	for _, testCase := range dataset.Cases {
		known[testCase.ID] = struct{}{}
		diagnosis, ok := diagnoses[testCase.ID]
		if !ok {
			return DatasetScore{}, fmt.Errorf("missing diagnosis for case %q", testCase.ID)
		}
		caseScore := ScoreCase(testCase, diagnosis)
		result.Cases = append(result.Cases, caseScore)
		result.Total += caseScore.Total
	}

	var unknown []string
	for caseID := range diagnoses {
		if _, ok := known[caseID]; !ok {
			unknown = append(unknown, caseID)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return DatasetScore{}, fmt.Errorf("diagnoses contain unknown case IDs: %s", strings.Join(unknown, ", "))
	}
	result.Total /= float64(len(result.Cases))
	return result, nil
}

func equalNormalized(want, got string) bool {
	return strings.EqualFold(strings.TrimSpace(want), strings.TrimSpace(got))
}

func boolPoint(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
