package logs

import (
	"regexp"
	"sort"
	"strings"
)

const DefaultMaxChars = 24_000

var (
	ansiPattern       = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	privateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [^-\n]*PRIVATE KEY-----.*?-----END [^-\n]*PRIVATE KEY-----`)
	secretPatterns    = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
		regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`),
		regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{16,}\b`),
		regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
		regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
		regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{12,}`),
	}
	passwordPattern  = regexp.MustCompile(`(?i)\b(password|passwd|pwd)\s*[:=]\s*[^\s,;]+`)
	importantPattern = regexp.MustCompile(`(?i)\b(error|failed|failure|panic|exception|denied|forbidden|unauthorized|timeout|timed out|not found|no such file|fatal|segmentation fault)\b`)
)

func Process(raw string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = DefaultMaxChars
	}
	sanitized := Sanitize(raw)
	if len(sanitized) <= maxChars {
		return sanitized
	}

	lines := strings.Split(sanitized, "\n")
	selected := make(map[int]struct{})
	for i, line := range lines {
		if importantPattern.MatchString(line) {
			for j := max(0, i-1); j <= min(len(lines)-1, i+1); j++ {
				selected[j] = struct{}{}
			}
		}
	}
	for i := max(0, len(lines)-120); i < len(lines); i++ {
		selected[i] = struct{}{}
	}

	indexes := make([]int, 0, len(selected))
	for i := range selected {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)

	var b strings.Builder
	b.WriteString("[Flakehound selected important lines and the log tail; omitted sections are not shown.]\n")
	last := -2
	for _, i := range indexes {
		line := lines[i]
		if b.Len()+len(line)+8 > maxChars {
			continue
		}
		if i > last+1 {
			b.WriteString("...\n")
		}
		b.WriteString(line)
		b.WriteByte('\n')
		last = i
	}
	return strings.TrimSpace(b.String())
}

func Sanitize(raw string) string {
	result := strings.ReplaceAll(raw, "\r\n", "\n")
	result = strings.ReplaceAll(result, "\r", "\n")
	result = ansiPattern.ReplaceAllString(result, "")
	result = privateKeyPattern.ReplaceAllString(result, "[REDACTED PRIVATE KEY]")
	for _, pattern := range secretPatterns {
		result = pattern.ReplaceAllString(result, "[REDACTED]")
	}
	result = passwordPattern.ReplaceAllString(result, `${1}=[REDACTED]`)
	return strings.TrimSpace(result)
}
