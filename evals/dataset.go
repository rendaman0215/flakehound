// Package evals provides deterministic, offline evaluation helpers for
// Flakehound diagnosis outputs.
package evals

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"strings"
)

const SchemaVersion = 1

var (
	caseIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	failureTypes  = stringSet(
		"test_failure",
		"dependency_failure",
		"permission_error",
		"infra_error",
		"flaky_suspected",
		"unknown",
	)
	retryableValues = stringSet("yes", "no", "unknown")
	ownerValues     = stringSet("app", "platform", "security", "unknown")
)

// Dataset is the versioned manifest for a collection of golden CI failures.
type Dataset struct {
	SchemaVersion int    `json:"schema_version"`
	Cases         []Case `json:"cases"`
}

// Case links an anonymized log fixture to its expected diagnosis fields.
type Case struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	LogFixture  string   `json:"log_fixture"`
	Tags        []string `json:"tags,omitempty"`
	Expectation Expected `json:"expected"`
}

// Expected contains the fields used for deterministic scoring.
type Expected struct {
	FailureType        string   `json:"failure_type"`
	Retryable          string   `json:"retryable"`
	OwnerHint          string   `json:"owner_hint"`
	EvidenceSubstrings []string `json:"evidence_substrings"`
}

// LoadDataset strictly decodes and validates a manifest. Fixture paths are
// resolved from the root of fsys, not from the process working directory.
func LoadDataset(fsys fs.FS, filename string) (Dataset, error) {
	data, err := fs.ReadFile(fsys, filename)
	if err != nil {
		return Dataset{}, fmt.Errorf("read dataset: %w", err)
	}

	var dataset Dataset
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dataset); err != nil {
		return Dataset{}, fmt.Errorf("decode dataset: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Dataset{}, fmt.Errorf("decode dataset: %w", err)
	}
	if err := ValidateDataset(fsys, dataset); err != nil {
		return Dataset{}, err
	}
	return dataset, nil
}

// ValidateDataset checks schema invariants and verifies that each expected
// evidence substring is grounded in its referenced fixture.
func ValidateDataset(fsys fs.FS, dataset Dataset) error {
	if dataset.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version = %d, want %d", dataset.SchemaVersion, SchemaVersion)
	}
	if len(dataset.Cases) == 0 {
		return errors.New("dataset must contain at least one case")
	}

	caseIDs := make(map[string]struct{}, len(dataset.Cases))
	fixtures := make(map[string]string, len(dataset.Cases))
	for index, testCase := range dataset.Cases {
		label := fmt.Sprintf("cases[%d]", index)
		if !caseIDPattern.MatchString(testCase.ID) {
			return fmt.Errorf("%s.id %q must match %s", label, testCase.ID, caseIDPattern)
		}
		if _, exists := caseIDs[testCase.ID]; exists {
			return fmt.Errorf("%s.id %q is duplicated", label, testCase.ID)
		}
		caseIDs[testCase.ID] = struct{}{}
		if strings.TrimSpace(testCase.Title) == "" {
			return fmt.Errorf("%s.title must not be empty", label)
		}
		if err := validateFixturePath(testCase.LogFixture); err != nil {
			return fmt.Errorf("%s.log_fixture: %w", label, err)
		}
		if previousID, exists := fixtures[testCase.LogFixture]; exists {
			return fmt.Errorf("%s.log_fixture %q is also used by case %q", label, testCase.LogFixture, previousID)
		}
		fixtures[testCase.LogFixture] = testCase.ID
		if err := validateExpected(testCase.Expectation); err != nil {
			return fmt.Errorf("%s.expected: %w", label, err)
		}

		fixture, err := fs.ReadFile(fsys, testCase.LogFixture)
		if err != nil {
			return fmt.Errorf("%s.log_fixture %q: %w", label, testCase.LogFixture, err)
		}
		if strings.TrimSpace(string(fixture)) == "" {
			return fmt.Errorf("%s.log_fixture %q is empty", label, testCase.LogFixture)
		}
		fixtureText := strings.ToLower(string(fixture))
		for _, evidence := range testCase.Expectation.EvidenceSubstrings {
			if !strings.Contains(fixtureText, strings.ToLower(evidence)) {
				return fmt.Errorf("%s evidence substring %q is not present in fixture %q", label, evidence, testCase.LogFixture)
			}
		}
	}
	return nil
}

func validateFixturePath(filename string) error {
	if filename == "" {
		return errors.New("must not be empty")
	}
	if strings.Contains(filename, `\`) || path.IsAbs(filename) || path.Clean(filename) != filename || filename == "." || strings.HasPrefix(filename, "../") {
		return fmt.Errorf("%q must be a clean relative slash-separated path", filename)
	}
	if path.Ext(filename) != ".log" {
		return fmt.Errorf("%q must have a .log extension", filename)
	}
	return nil
}

func validateExpected(expected Expected) error {
	if !contains(failureTypes, expected.FailureType) {
		return fmt.Errorf("failure_type %q is invalid", expected.FailureType)
	}
	if !contains(retryableValues, expected.Retryable) {
		return fmt.Errorf("retryable %q is invalid", expected.Retryable)
	}
	if !contains(ownerValues, expected.OwnerHint) {
		return fmt.Errorf("owner_hint %q is invalid", expected.OwnerHint)
	}
	if len(expected.EvidenceSubstrings) == 0 {
		return errors.New("evidence_substrings must not be empty")
	}
	seen := make(map[string]struct{}, len(expected.EvidenceSubstrings))
	for index, evidence := range expected.EvidenceSubstrings {
		normalized := strings.ToLower(strings.TrimSpace(evidence))
		if normalized == "" {
			return fmt.Errorf("evidence_substrings[%d] must not be empty", index)
		}
		if _, exists := seen[normalized]; exists {
			return fmt.Errorf("evidence substring %q is duplicated", evidence)
		}
		seen[normalized] = struct{}{}
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("multiple JSON values are not allowed")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func contains(values map[string]struct{}, value string) bool {
	_, ok := values[value]
	return ok
}
