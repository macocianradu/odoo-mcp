package runbot

import (
	"fmt"
	"regexp"
	"strings"
)

// Snippet is a contiguous excerpt of a log file.
type Snippet struct {
	StartLine int      `json:"start_line"` // 1-based
	Lines     []string `json:"lines"`
	Reason    string   `json:"reason"`
}

// Extraction is the failure digest for a single log step.
type Extraction struct {
	Step       string    `json:"step"`
	URL        string    `json:"url"`
	TotalLines int       `json:"total_lines"`
	Snippets   []Snippet `json:"snippets,omitempty"`
	Note       string    `json:"note,omitempty"`
}

// ExtractOptions tunes how much of a log is returned. Zero values are replaced
// by the defaults, which are sized to stay well inside a model's context.
type ExtractOptions struct {
	ContextBefore int
	MaxSnippets   int
	MaxLines      int
	TailLines     int
}

func (o ExtractOptions) withDefaults() ExtractOptions {
	if o.ContextBefore <= 0 {
		o.ContextBefore = 3
	}
	if o.MaxSnippets <= 0 {
		o.MaxSnippets = 5
	}
	if o.MaxLines <= 0 {
		o.MaxLines = 300
	}
	if o.TailLines <= 0 {
		o.TailLines = 40
	}
	return o
}

// odooRecord matches the start of an Odoo log record:
//
//	2026-09-09 11:47:15,234 26 ERROR 124593755-master-all odoo.addons.x: message
//
// Anything not matching it is a continuation of the preceding record, which is
// where tracebacks and assertion messages live.
var odooRecord = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d+ \d+ ([A-Z]+) `)

// genericFailure matches failure markers in logs that are not Odoo-formatted,
// such as linter, semgrep or shell output.
var genericFailure = regexp.MustCompile(`(?i)(\btraceback\b|\bfatal\b|\bexception\b|\berror[s]?\b|\bfail(ed|ure)?\b|✖|✗|\b\d+ (findings|problems|errors)\b)`)

// Extract builds a failure digest from a log file.
//
// Odoo logs are handled record-wise: a failing record carries its continuation
// lines, so a traceback and the assertion message that follows it stay together.
// Logs in any other format fall back to marker matching, and a log with no
// recognisable markers falls back to its tail — a build can fail without ever
// writing the word "error" (semgrep reports findings to a JSON file), so
// returning nothing would be worse than returning the end of the run.
func Extract(step, url string, lines []string, opts ExtractOptions) Extraction {
	opts = opts.withDefaults()
	ex := Extraction{Step: step, URL: url, TotalLines: len(lines)}
	if len(lines) == 0 {
		ex.Note = "log is empty"
		return ex
	}

	ranges := failureRanges(lines)
	if len(ranges) == 0 {
		ex.Snippets = []Snippet{tail(lines, opts.TailLines)}
		ex.Note = "no failure markers found; showing the end of the log"
		return ex
	}

	ex.Snippets = buildSnippets(lines, ranges, opts)
	if dropped := len(ranges) - len(ex.Snippets); dropped > 0 {
		ex.Note = fmt.Sprintf("%d further match(es) omitted; see the full log", dropped)
	}
	return ex
}

// lineRange is a half-open [start, end) range of line indices.
type lineRange struct {
	start, end int
	reason     string
}

func failureRanges(lines []string) []lineRange {
	if isOdooLog(lines) {
		return odooFailureRanges(lines)
	}
	return genericFailureRanges(lines)
}

// isOdooLog reports whether the file looks like Odoo server output. Only the
// first lines are examined so a stray timestamp deep in a lint log does not
// switch strategies.
func isOdooLog(lines []string) bool {
	limit := min(len(lines), 200)
	for i := range limit {
		if odooRecord.MatchString(lines[i]) {
			return true
		}
	}
	return false
}

// odooFailureRanges returns each failing record together with its continuation.
func odooFailureRanges(lines []string) []lineRange {
	var out []lineRange
	for i := 0; i < len(lines); i++ {
		m := odooRecord.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		level := m[1]
		if level != "ERROR" && level != "CRITICAL" {
			continue
		}
		// Absorb continuation lines: everything up to the next record.
		end := i + 1
		for end < len(lines) && !odooRecord.MatchString(lines[end]) {
			end++
		}
		out = append(out, lineRange{start: i, end: end, reason: level + " record with its traceback"})
		i = end - 1
	}
	return out
}

func genericFailureRanges(lines []string) []lineRange {
	var out []lineRange
	for i := 0; i < len(lines); i++ {
		if !genericFailure.MatchString(lines[i]) {
			continue
		}
		end := i + 1
		// A traceback header pulls in its indented frames.
		if strings.Contains(strings.ToLower(lines[i]), "traceback") {
			for end < len(lines) && (lines[end] == "" || strings.HasPrefix(lines[end], " ") || strings.HasPrefix(lines[end], "\t")) {
				end++
			}
			if end < len(lines) {
				end++ // the exception line itself
			}
		}
		out = append(out, lineRange{start: i, end: end, reason: "failure marker"})
		i = end - 1
	}
	return out
}

// buildSnippets turns ranges into snippets, adding leading context, merging
// overlaps, and enforcing the snippet and line budgets.
func buildSnippets(lines []string, ranges []lineRange, opts ExtractOptions) []Snippet {
	var merged []lineRange
	for _, r := range ranges {
		r.start = max(0, r.start-opts.ContextBefore)
		if n := len(merged); n > 0 && r.start <= merged[n-1].end {
			if r.end > merged[n-1].end {
				merged[n-1].end = r.end
			}
			continue
		}
		merged = append(merged, r)
	}

	var out []Snippet
	budget := opts.MaxLines
	for _, r := range merged {
		if len(out) >= opts.MaxSnippets || budget <= 0 {
			break
		}
		end := min(r.end, r.start+budget)
		// The truncation marker occupies a line of the budget itself, so make
		// room for it rather than overshooting by one.
		truncated := end < r.end
		if truncated && end > r.start {
			end--
		}
		seg := append([]string(nil), lines[r.start:end]...)
		if truncated {
			seg = append(seg, fmt.Sprintf("[... %d more lines truncated ...]", r.end-end))
		}
		budget -= len(seg)
		out = append(out, Snippet{StartLine: r.start + 1, Lines: seg, Reason: r.reason})
	}
	return out
}

func tail(lines []string, n int) Snippet {
	start := max(0, len(lines)-n)
	return Snippet{
		StartLine: start + 1,
		Lines:     append([]string(nil), lines[start:]...),
		Reason:    "end of log",
	}
}
