package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rendaman0215/flakehound/internal/diagnosis"
	gh "github.com/rendaman0215/flakehound/internal/github"
	"github.com/rendaman0215/flakehound/internal/llm/anthropic"
	"github.com/rendaman0215/flakehound/internal/llm/openai"
	logprocessor "github.com/rendaman0215/flakehound/internal/logs"
	"github.com/rendaman0215/flakehound/internal/render"
)

type getenvFunc func(string) string

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv getenvFunc, version string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		return writeUsage(stdout)
	}
	if args[0] == "version" || args[0] == "--version" {
		_, err := fmt.Fprintln(stdout, version)
		return err
	}
	if len(args) < 2 || args[0] != "sniff" {
		return fmt.Errorf("unknown command; expected 'sniff log' or 'sniff github'")
	}

	switch args[1] {
	case "log":
		return runLog(ctx, args[2:], stdout, stderr, getenv)
	case "github":
		return runGitHub(ctx, args[2:], stdout, stderr, getenv)
	default:
		return fmt.Errorf("unknown sniff target %q; expected 'log' or 'github'", args[1])
	}
}

func runLog(ctx context.Context, args []string, stdout, stderr io.Writer, getenv getenvFunc) error {
	flags := flag.NewFlagSet("sniff log", flag.ContinueOnError)
	flags.SetOutput(stderr)
	logFile := flags.String("log-file", "", "path to the log file")
	provider := flags.String("provider", "openai", "LLM provider: openai or anthropic")
	model := flags.String("model", "", "provider model name")
	maxChars := flags.Int("max-log-chars", logprocessor.DefaultMaxChars, "maximum sanitized log characters sent to the LLM")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *logFile == "" {
		return fmt.Errorf("--log-file is required")
	}
	raw, err := os.ReadFile(*logFile)
	if err != nil {
		return fmt.Errorf("read log file: %w", err)
	}
	diagnoser, err := newDiagnoser(*provider, *model, getenv)
	if err != nil {
		return err
	}
	result, err := diagnoser.Diagnose(ctx, diagnosis.DiagnosisInput{Log: logprocessor.Process(string(raw), *maxChars)})
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, render.Markdown(result))
	return err
}

func runGitHub(ctx context.Context, args []string, stdout, stderr io.Writer, getenv getenvFunc) error {
	flags := flag.NewFlagSet("sniff github", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "GitHub repository in owner/repo form")
	runID := flags.Int64("run-id", 0, "GitHub Actions workflow run ID")
	provider := flags.String("provider", "openai", "LLM provider: openai or anthropic")
	model := flags.String("model", "", "provider model name")
	comment := flags.Bool("comment", false, "post the diagnosis to the related pull request")
	prNumber := flags.Int("pr-number", 0, "pull request number (overrides workflow metadata)")
	maxChars := flags.Int("max-log-chars", logprocessor.DefaultMaxChars, "maximum sanitized log characters sent to the LLM")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if !validRepo(*repo) {
		return fmt.Errorf("--repo must use owner/repo format")
	}
	if *runID <= 0 {
		return fmt.Errorf("--run-id must be a positive integer")
	}
	if *prNumber < 0 {
		return fmt.Errorf("--pr-number cannot be negative")
	}

	githubClient := gh.NewWithOptions(getenv("GITHUB_TOKEN"), getenv("FLAKEHOUND_GITHUB_API_URL"), nil)
	run, err := githubClient.GetRun(ctx, *repo, *runID)
	if err != nil {
		return err
	}
	failedJobs, err := githubClient.FailedJobs(ctx, *repo, *runID)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "flakehound: warning: could not list failed jobs: %v\n", err); writeErr != nil {
			return writeErr
		}
	}
	rawLogs, err := githubClient.DownloadLogs(ctx, *repo, *runID)
	if err != nil {
		return err
	}
	diagnoser, err := newDiagnoser(*provider, *model, getenv)
	if err != nil {
		return err
	}
	result, err := diagnoser.Diagnose(ctx, diagnosis.DiagnosisInput{
		Repo:       *repo,
		RunID:      *runID,
		RunURL:     run.HTMLURL,
		Workflow:   run.Name,
		FailedJobs: failedJobs,
		Log:        logprocessor.Process(rawLogs, *maxChars),
	})
	if err != nil {
		return err
	}
	markdown := render.Markdown(result)
	if _, err := io.WriteString(stdout, markdown); err != nil {
		return err
	}
	if summaryPath := getenv("GITHUB_STEP_SUMMARY"); summaryPath != "" {
		if err := appendFile(summaryPath, markdown); err != nil {
			return fmt.Errorf("write GitHub Job Summary: %w", err)
		}
	}

	if !*comment {
		return nil
	}
	resolvedPR := *prNumber
	if resolvedPR == 0 && len(run.PullRequests) > 0 {
		resolvedPR = run.PullRequests[0].Number
	}
	if resolvedPR == 0 {
		_, err := fmt.Fprintln(stderr, "flakehound: PR comment skipped: no PR number was provided or found in workflow run metadata")
		return err
	}
	if err := githubClient.CreatePRComment(ctx, *repo, resolvedPR, markdown); err != nil {
		_, writeErr := fmt.Fprintf(stderr, "flakehound: warning: diagnosis was generated, but the PR comment could not be posted: %v\n", err)
		return writeErr
	}
	_, err = fmt.Fprintf(stderr, "flakehound: posted diagnosis to PR #%d\n", resolvedPR)
	return err
}

func newDiagnoser(provider, model string, getenv getenvFunc) (diagnosis.Diagnoser, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		return openai.NewWithOptions(getenv("OPENAI_API_KEY"), model, getenv("FLAKEHOUND_OPENAI_BASE_URL"), nil), nil
	case "anthropic":
		return anthropic.NewWithOptions(getenv("ANTHROPIC_API_KEY"), model, getenv("FLAKEHOUND_ANTHROPIC_BASE_URL"), nil), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q; expected openai or anthropic", provider)
	}
}

func validRepo(repo string) bool {
	parts := strings.Split(repo, "/")
	return len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != ""
}

func appendFile(filename, content string) error {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err = io.WriteString(file, content+"\n"); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprintln(w, `Flakehound sniffs broken CI logs and tells developers what to do next.

Usage:
  flakehound sniff log --log-file FILE [--provider openai|anthropic] [--model MODEL]
  flakehound sniff github --repo OWNER/REPO --run-id ID [--provider PROVIDER] [--model MODEL] [--comment] [--pr-number N]
  flakehound version

API keys are read only from OPENAI_API_KEY, ANTHROPIC_API_KEY, and GITHUB_TOKEN.`)
	return err
}
