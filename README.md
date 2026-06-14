# Flakehound

Flakehound is a tiny SRE hound that sniffs broken CI logs and tells developers what to do next.

It reads failed GitHub Actions logs, removes obvious secrets, keeps useful failure evidence, asks OpenAI or Anthropic for a structured diagnosis, and renders the result as Markdown in the GitHub Actions Job Summary and optionally on the pull request.

Flakehound is not only a log summarizer. The goal is a small **Developer Feedback Plane** that answers:

- What happened?
- What should the developer do next?
- Is a retry likely to help?
- Should the application, platform, or security team investigate?

> [!IMPORTANT]
> Sanitized CI log fragments are sent to the selected LLM provider. The MVP redacts common credentials, but secret detection is not perfect. Review your logs and provider data policy before enabling Flakehound on sensitive workloads.

## Quick start

### Local CLI

Build from source:

```bash
go install github.com/rendaman0215/flakehound/cmd/flakehound@latest
```

Diagnose a local log with OpenAI:

```bash
export OPENAI_API_KEY="..."
flakehound sniff log \
  --log-file ./examples/failure.log \
  --provider openai \
  --model gpt-5.2
```

Diagnose it with Anthropic:

```bash
export ANTHROPIC_API_KEY="..."
flakehound sniff log \
  --log-file ./examples/failure.log \
  --provider anthropic \
  --model claude-sonnet-4-5
```

API keys are read only from environment variables. There are no API-key CLI flags.

| Provider  | Provider name | Environment variable | CLI default model   |
| --------- | ------------- | -------------------- | ------------------- |
| OpenAI    | `openai`      | `OPENAI_API_KEY`     | `gpt-5.2`           |
| Anthropic | `anthropic`   | `ANTHROPIC_API_KEY`  | `claude-sonnet-4-5` |

Model names and availability change. Pass `--model` explicitly when you need a stable organization-approved model.

### GitHub workflow run

```bash
export GITHUB_TOKEN="..."
export OPENAI_API_KEY="..."

flakehound sniff github \
  --repo owner/repo \
  --run-id 123456789 \
  --provider openai \
  --model gpt-5.2 \
  --comment
```

Use `--pr-number 123` when the workflow run metadata does not contain a pull request. Without a resolvable PR number, Flakehound logs why the comment was skipped and still writes the Job Summary when `GITHUB_STEP_SUMMARY` is available.

## GitHub Action

The Composite Action downloads a prebuilt Flakehound binary from GitHub Releases and verifies it against the release SHA-256 checksum before extraction. The consuming repository does not need Go, Node.js, or Docker.

```yaml
name: Flakehound

on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]

permissions:
  actions: read
  contents: read
  issues: write
  pull-requests: read

jobs:
  sniff:
    if: ${{ github.event.workflow_run.conclusion == 'failure' }}
    runs-on: ubuntu-latest
    steps:
      - uses: rendaman0215/flakehound@v0
        with:
          repo: ${{ github.repository }}
          run-id: ${{ github.event.workflow_run.id }}
          provider: openai
          model: gpt-5.2
          comment: true
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
```

Anthropic uses the same Action:

```yaml
- uses: rendaman0215/flakehound@v0
  with:
    repo: ${{ github.repository }}
    run-id: ${{ github.event.workflow_run.id }}
    provider: anthropic
    model: claude-sonnet-4-5
    comment: true
  env:
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
```

The Action supports Linux and macOS runners on x64 and ARM64. `version` defaults to the latest release; set it to a release tag such as `v0.1.0` to pin the downloaded CLI.

### Required permissions

| Permission            | Why                                                |
| --------------------- | -------------------------------------------------- |
| `actions: read`       | Download workflow run logs and list failed jobs.   |
| `contents: read`      | Standard minimal repository access.                |
| `issues: write`       | Create a PR issue comment when `comment: true`.    |
| `pull-requests: read` | Read pull request context associated with the run. |

The workflow intentionally does not check out or execute code from the failed pull request.

## Usage scenarios

### 1. Automatically comment on a PR with failed CI

A platform team installs the `workflow_run` workflow in repositories. When CI fails, Flakehound reads the logs and comments with the likely cause, retry guidance, next actions, owner hint, and evidence.

This reduces time spent reading long logs and helps developers decide whether to retry or ask the platform team.

### 2. Diagnose a saved log locally

Developers can save any CI or build log and run:

```bash
flakehound sniff log --log-file failure.log --provider openai
```

This works outside GitHub Actions and provides a low-risk way to evaluate Flakehound before repository-wide adoption.

### 3. Distribute an organization-standard workflow

A platform team can publish the `workflow_run` example as an organization workflow template. Application teams then need only a small, consistent integration while the platform team gets fewer first-line log-triage questions.

## How it works

1. Fetch the workflow run metadata, failed job names, and log archive from GitHub.
2. Strip ANSI control sequences and redact common GitHub, OpenAI, Anthropic, AWS, bearer, password, and private-key patterns.
3. For long logs, prioritize lines containing failure terms and retain the log tail.
4. Send metadata and sanitized evidence to the selected provider.
5. Parse the provider's JSON diagnosis. Invalid JSON falls back to the raw text with `unknown` classifications and confidence `0.3`.
6. Render Markdown owned by Flakehound, not by the model.
7. Print the diagnosis, append it to the Job Summary, and optionally create a PR comment containing `<!-- flakehound-diagnosis -->`.

The main responsibilities are separated under `internal/diagnosis`, `internal/logs`, `internal/llm`, `internal/github`, `internal/render`, and `internal/app` so future rules and routing do not need to be embedded in provider clients.

## Development

```bash
go version # Go 1.26.4 or newer
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/flakehound
```

The application intentionally uses only the Go standard library at runtime. There are currently no third-party Go module dependencies to update.

CI also enforces formatting, clean module files, a 60% coverage floor, `golangci-lint`, `govulncheck`, GitHub Actions validation, all release target builds, and a GoReleaser snapshot build.

Dependency and tool updates are managed by Renovate through `renovate.json`. Install the Renovate GitHub App for the repository; it will update Go versions, GitHub Action digests, Go modules, and pinned CI tool versions on the configured weekly schedule.

### Repository dogfooding

This repository runs Flakehound against its own failed `CI` workflow through `.github/workflows/flakehound.yml`. Add `OPENAI_API_KEY` as a repository Actions secret to enable diagnosis and PR comments. Runs originating from forks are intentionally skipped so untrusted workflows cannot consume the repository's LLM credentials.

GitHub only emits `workflow_run` events to workflows that already exist on the default branch. Therefore, the pull request that initially adds this dogfooding workflow cannot diagnose its own CI failures. After the workflow is merged, future failures are diagnosed automatically. A previous failed run can also be diagnosed from **Actions > Flakehound > Run workflow** by entering its workflow run ID.

For source-based development before a release exists:

```bash
go run ./cmd/flakehound sniff log \
  --log-file examples/failure.log \
  --provider openai
```

Tag releases such as `v0.1.0` to run the included GoReleaser workflow. Its asset names match those expected by `action.yml`.


## License

MIT
