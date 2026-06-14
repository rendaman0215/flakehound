# Flakehound Golden Evaluations

This directory is an offline foundation for measuring Flakehound diagnosis
quality. It does not call an LLM, import a provider client, or require network
access.

## Run

From the repository root:

```bash
go test ./evals/...
```

`go test ./...` also includes these checks.

## Dataset layout

`testdata/dataset.json` is the versioned manifest. Each case references one
anonymized, synthetic CI log under `testdata/fixtures/` and records:

- `failure_type`: one of Flakehound's diagnosis failure types.
- `retryable`: `yes`, `no`, or `unknown`.
- `owner_hint`: `app`, `platform`, `security`, or `unknown`.
- `evidence_substrings`: exact fragments that a grounded diagnosis should
  include in its `evidence` entries.

JSON was chosen to keep validation in the Go standard library. Increment
`schema_version` when making a breaking format change and update the validator
in the same change.

The committed cases cover deterministic test failures, dependency outages,
permission errors, runner infrastructure failures, and suspected flaky tests.
Fixtures must remain synthetic and anonymized: use reserved domains such as
`example.invalid`, generic repository names, and no real credentials or IDs.

## Validation guarantees

`LoadDataset` uses strict JSON decoding and then `ValidateDataset` checks:

- supported schema version and enum values;
- non-empty, unique case IDs and fixture paths;
- clean relative `.log` paths with no traversal;
- non-empty fixtures and evidence expectations;
- every expected evidence substring actually occurs in the referenced log.

The golden test also requires coverage of the five primary non-unknown failure
types. Adding a case therefore requires only a fixture and a manifest entry.

## Scoring captured diagnoses

Use `ScoreCase` for one model response or `ScoreDataset` for a complete captured
run. A diagnosis supplies only the fields used by this evaluation:

```go
score := evals.ScoreCase(testCase, evals.Diagnosis{
    FailureType: "dependency_failure",
    Retryable:   "yes",
    OwnerHint:   "platform",
    Evidence:    []string{"registry response: 503 Service Unavailable"},
})
```

The score is deterministic and ranges from `0` to `1`. Failure type,
retryability, owner, and evidence coverage each contribute 25%. Evidence
coverage is the fraction of expected substrings found, case-insensitively,
across all supplied evidence entries.

`ScoreDataset` requires exactly one diagnosis for every known case and rejects
unknown case IDs. This makes it suitable for evaluating JSON responses captured
by a separate, explicitly online model-runner without letting incomplete runs
silently raise the aggregate score.
