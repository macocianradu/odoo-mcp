package runbot

import (
	"os"
	"strings"
	"testing"
)

func loadLog(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading log fixture: %v", err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func snippetText(ex Extraction) string {
	var sb strings.Builder
	for _, s := range ex.Snippets {
		for _, l := range s.Lines {
			sb.WriteString(l)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// The Odoo path must keep a failing record together with the continuation lines
// that follow it, because the assertion message carrying the real cause is
// printed after the traceback rather than inside it.
func TestExtractOdooTestFailure(t *testing.T) {
	lines := loadLog(t, "log_lint_fail.txt")
	if !isOdooLog(lines) {
		t.Fatal("lint log not detected as Odoo-formatted")
	}
	ex := Extract("start_test_lint", "http://example/log.txt", lines, ExtractOptions{})
	if len(ex.Snippets) == 0 {
		t.Fatal("no snippets extracted from a failing log")
	}
	got := snippetText(ex)
	for _, want := range []string{
		"FAIL: TestESLint.test_eslint",
		"Traceback (most recent call last):",
		"AssertionError: 1 != 0",
		"no-unused-vars", // the actual offending lint finding
		"tour_plugin.js", // the file at fault
	} {
		if !strings.Contains(got, want) {
			t.Errorf("extraction missing %q", want)
		}
	}
	if ex.Note != "" {
		t.Errorf("unexpected note %q", ex.Note)
	}
}

// A build can fail without its log containing the word "error": semgrep writes
// findings to a JSON file and only summarises them in the log.
func TestExtractSemgrepFindings(t *testing.T) {
	ex := Extract("check_semgrep_security", "u", loadLog(t, "log_semgrep.txt"), ExtractOptions{})
	if len(ex.Snippets) == 0 {
		t.Fatal("no snippets from semgrep log")
	}
	if got := snippetText(ex); !strings.Contains(got, "10 findings") {
		t.Errorf("extraction missed the findings summary:\n%s", got)
	}
}

func TestExtractFallsBackToTail(t *testing.T) {
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = "everything is completely fine here"
	}
	ex := Extract("s", "u", lines, ExtractOptions{TailLines: 10})
	if len(ex.Snippets) != 1 {
		t.Fatalf("got %d snippets, want 1 tail snippet", len(ex.Snippets))
	}
	s := ex.Snippets[0]
	if len(s.Lines) != 10 {
		t.Errorf("tail has %d lines, want 10", len(s.Lines))
	}
	if s.StartLine != 491 {
		t.Errorf("tail starts at %d, want 491", s.StartLine)
	}
	if ex.Note == "" {
		t.Error("expected a note explaining the fallback")
	}
}

func TestExtractEmptyLog(t *testing.T) {
	ex := Extract("s", "u", nil, ExtractOptions{})
	if len(ex.Snippets) != 0 || ex.Note == "" {
		t.Errorf("empty log: snippets=%d note=%q", len(ex.Snippets), ex.Note)
	}
}

// The line budget is what keeps a multi-megabyte log from reaching the model.
func TestExtractRespectsLineBudget(t *testing.T) {
	var lines []string
	for i := range 40 {
		lines = append(lines, "2026-09-09 11:47:15,234 26 ERROR db logger: failure number "+string(rune('a'+i%26)))
		lines = append(lines, "  continuation")
	}
	ex := Extract("s", "u", lines, ExtractOptions{MaxSnippets: 3, MaxLines: 20, ContextBefore: 0})
	if len(ex.Snippets) > 3 {
		t.Errorf("got %d snippets, want at most 3", len(ex.Snippets))
	}
	total := 0
	for _, s := range ex.Snippets {
		total += len(s.Lines)
	}
	if total > 20 {
		t.Errorf("returned %d lines, want at most 20", total)
	}
	if ex.Note == "" {
		t.Error("expected a note when matches were omitted")
	}
}

func TestExtractTruncationMarker(t *testing.T) {
	lines := []string{"2026-09-09 11:47:15,234 26 ERROR db l: boom"}
	for range 50 {
		lines = append(lines, "  continuation line")
	}
	ex := Extract("s", "u", lines, ExtractOptions{MaxLines: 5, ContextBefore: 0})
	got := snippetText(ex)
	if !strings.Contains(got, "more lines truncated") {
		t.Errorf("expected a truncation marker, got:\n%s", got)
	}
}

func TestIsOdooLog(t *testing.T) {
	if isOdooLog([]string{"just some text", "no timestamps here"}) {
		t.Error("plain text detected as Odoo log")
	}
	if !isOdooLog([]string{"2026-09-09 11:47:15,234 26 INFO db logger: hi"}) {
		t.Error("Odoo record not detected")
	}
	// A timestamp appearing only far into the file must not switch strategies.
	long := make([]string, 300)
	for i := range long {
		long[i] = "plain"
	}
	long[250] = "2026-09-09 11:47:15,234 26 INFO db logger: hi"
	if isOdooLog(long) {
		t.Error("late timestamp should not classify the log as Odoo-formatted")
	}
}
