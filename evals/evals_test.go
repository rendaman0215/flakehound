package evals

import (
	"embed"
	"io/fs"
	"math"
	"testing"
)

//go:embed testdata/dataset.json testdata/fixtures/*.log
var testdata embed.FS

func TestGoldenDataset(t *testing.T) {
	dataFS := goldenFS(t)
	dataset, err := LoadDataset(dataFS, "dataset.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(dataset.Cases) != 5 {
		t.Fatalf("case count = %d, want 5", len(dataset.Cases))
	}

	wantTypes := map[string]bool{
		"test_failure":       false,
		"dependency_failure": false,
		"permission_error":   false,
		"infra_error":        false,
		"flaky_suspected":    false,
	}
	for _, testCase := range dataset.Cases {
		wantTypes[testCase.Expectation.FailureType] = true
	}
	for failureType, found := range wantTypes {
		if !found {
			t.Errorf("dataset does not cover %q", failureType)
		}
	}
}

func TestScoreCase(t *testing.T) {
	dataset, err := LoadDataset(goldenFS(t), "dataset.json")
	if err != nil {
		t.Fatal(err)
	}
	testCase := dataset.Cases[0]

	score := ScoreCase(testCase, Diagnosis{
		FailureType: " TEST_FAILURE ",
		Retryable:   "no",
		OwnerHint:   "platform",
		Evidence:    []string{"Assertion details: expected status 204, got 500"},
	})
	if !score.FailureTypeMatch || !score.RetryableMatch || score.OwnerHintMatch {
		t.Fatalf("unexpected classification score: %+v", score)
	}
	if score.EvidenceMatched != 1 || score.EvidenceExpected != 2 {
		t.Fatalf("unexpected evidence score: %+v", score)
	}
	if math.Abs(score.Total-0.625) > 0.000001 {
		t.Fatalf("total = %f, want 0.625", score.Total)
	}
}

func TestScoreDatasetRequiresEveryKnownCase(t *testing.T) {
	dataset, err := LoadDataset(goldenFS(t), "dataset.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ScoreDataset(dataset, map[string]Diagnosis{}); err == nil {
		t.Fatal("ScoreDataset accepted missing diagnoses")
	}
}

func TestScoreDatasetRejectsEmptyDataset(t *testing.T) {
	if _, err := ScoreDataset(Dataset{}, map[string]Diagnosis{}); err == nil {
		t.Fatal("ScoreDataset accepted an empty dataset")
	}
}

func TestValidateDatasetRejectsUngroundedEvidence(t *testing.T) {
	dataset, err := LoadDataset(goldenFS(t), "dataset.json")
	if err != nil {
		t.Fatal(err)
	}
	dataset.Cases[0].Expectation.EvidenceSubstrings[0] = "line absent from fixture"
	if err := ValidateDataset(goldenFS(t), dataset); err == nil {
		t.Fatal("ValidateDataset accepted ungrounded evidence")
	}
}

func goldenFS(t *testing.T) fs.FS {
	t.Helper()
	result, err := fs.Sub(testdata, "testdata")
	if err != nil {
		t.Fatal(err)
	}
	return result
}
