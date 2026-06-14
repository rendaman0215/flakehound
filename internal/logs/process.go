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
		regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\b`),
	}
	passwordPattern            = regexp.MustCompile(`(?i)\b(password|passwd|pwd)\s*[:=]\s*[^\s,;]+`)
	sensitiveAssignmentPattern = regexp.MustCompile(`(?m)^(\s*(?:export\s+)?(?:(?:KEY|TOKEN|SECRET)|[A-Z][A-Z0-9_]*(?:KEY|TOKEN|SECRET)[A-Z0-9_]*|(?:key|token|secret)|(?:[a-z][a-z0-9]*[_-])+(?:key|token|secret)|api_key|access_token|refresh_token|auth_token|client_secret|secret_key)\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;]+)`)
	importantPattern           = regexp.MustCompile(`(?i)\b(error|failed|failure|panic|exception|denied|forbidden|unauthorized|timeout|timed out|not found|no such file|fatal|segmentation fault)\b`)
	sectionHeaderPattern       = regexp.MustCompile(`^===== (.+) =====$`)
	normalizeJobPattern        = regexp.MustCompile(`[^a-z0-9]+`)
)

type importantChunk struct {
	start         int
	end           int
	sectionHeader int
	priority      int
}

func Process(raw string, maxChars int, failedJobs ...string) string {
	if maxChars <= 0 {
		maxChars = DefaultMaxChars
	}
	sanitized := Sanitize(raw)
	if len(sanitized) <= maxChars {
		return sanitized
	}

	const notice = "[Flakehound selected important lines and the log tail; omitted sections are not shown.]\n"
	if maxChars <= len(notice) {
		return strings.TrimSpace(notice[:maxChars])
	}

	lines := strings.Split(sanitized, "\n")
	available := maxChars - len(notice)
	tailBudget := max(1, available/3)
	tail, tailStart := renderTail(lines, tailBudget)
	importantBudget := available - len(tail)
	important := renderImportant(lines, tailStart, importantBudget, failedJobs)

	return strings.TrimSpace(notice + important + tail)
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
	result = sensitiveAssignmentPattern.ReplaceAllString(result, `${1}[REDACTED]`)
	return strings.TrimSpace(result)
}

func renderTail(lines []string, budget int) (string, int) {
	if budget <= 0 || len(lines) == 0 {
		return "", len(lines)
	}

	start := len(lines)
	used := 0
	for i := len(lines) - 1; i >= 0 && len(lines)-i <= 120; i-- {
		lineSize := len(lines[i]) + 1
		prefixSize := 0
		if i > 0 {
			prefixSize = len("...\n")
		}
		if used+lineSize+prefixSize > budget {
			break
		}
		start = i
		used += lineSize
	}
	if start == len(lines) {
		last := lines[len(lines)-1]
		if budget > len("...\n") {
			keep := min(len(last), budget-len("...\n"))
			return "...\n" + last[len(last)-keep:], len(lines) - 1
		}
		return strings.Repeat(".", budget), len(lines) - 1
	}

	var b strings.Builder
	if start > 0 {
		b.WriteString("...\n")
	}
	for _, line := range lines[start:] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), start
}

func renderImportant(lines []string, end, budget int, failedJobs []string) string {
	if budget <= len("...\n") {
		return ""
	}

	normalizedFailedJobs := make([]string, 0, len(failedJobs))
	for _, name := range failedJobs {
		if normalized := normalizeJobName(name); normalized != "" {
			normalizedFailedJobs = append(normalizedFailedJobs, normalized)
		}
	}

	chunks := make([]importantChunk, 0)
	sectionHeader := -1
	sectionFailed := false
	failedSectionHeaders := make([]int, 0)
	for i := 0; i < end; i++ {
		if match := sectionHeaderPattern.FindStringSubmatch(lines[i]); match != nil {
			sectionHeader = i
			sectionFailed = matchesFailedJob(match[1], normalizedFailedJobs)
			if sectionFailed {
				failedSectionHeaders = append(failedSectionHeaders, i)
			}
		}
		if importantPattern.MatchString(lines[i]) {
			priority := 2
			if sectionFailed {
				priority = 0
			}
			chunks = append(chunks, importantChunk{
				start:         max(0, i-1),
				end:           min(end-1, i+1),
				sectionHeader: sectionHeader,
				priority:      priority,
			})
		}
	}
	for _, header := range failedSectionHeaders {
		sectionEnd := end - 1
		for i := header + 1; i < end; i++ {
			if sectionHeaderPattern.MatchString(lines[i]) {
				sectionEnd = i - 1
				break
			}
		}
		chunks = append(chunks, importantChunk{
			start:         max(header+1, sectionEnd-2),
			end:           sectionEnd,
			sectionHeader: header,
			priority:      1,
		})
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		if chunks[i].priority != chunks[j].priority {
			return chunks[i].priority < chunks[j].priority
		}
		return chunks[i].start < chunks[j].start
	})

	var b strings.Builder
	emitted := make(map[int]struct{})
	for _, chunk := range chunks {
		indexes := make([]int, 0, chunk.end-chunk.start+2)
		if chunk.sectionHeader >= 0 && chunk.sectionHeader < chunk.start {
			indexes = append(indexes, chunk.sectionHeader)
		}
		for i := chunk.start; i <= chunk.end; i++ {
			indexes = append(indexes, i)
		}

		wrote := false
		last := -2
		for _, i := range indexes {
			if _, ok := emitted[i]; ok {
				continue
			}
			separator := ""
			if !wrote || i > last+1 {
				separator = "...\n"
			}
			addition := separator + lines[i] + "\n"
			if b.Len()+len(addition) > budget {
				continue
			}
			b.WriteString(addition)
			emitted[i] = struct{}{}
			wrote = true
			last = i
		}
	}
	return b.String()
}

func normalizeJobName(name string) string {
	return strings.Trim(normalizeJobPattern.ReplaceAllString(strings.ToLower(name), " "), " ")
}

func matchesFailedJob(section string, failedJobs []string) bool {
	normalizedSection := normalizeJobName(section)
	for _, failedJob := range failedJobs {
		if strings.Contains(normalizedSection, failedJob) || strings.Contains(failedJob, normalizedSection) {
			return true
		}
	}
	return false
}
