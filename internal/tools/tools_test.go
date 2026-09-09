package tools

import (
	"strings"
	"testing"

	"github.com/odoo/odoo-mcp/internal/runbot"
)

func TestResultLabel(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build *runbot.Build
		want  string
	}{
		{"nil", nil, "no build"},
		{"passed", &runbot.Build{GlobalState: "done", GlobalResult: "ok"}, "ok"},
		{"failed", &runbot.Build{GlobalState: "done", GlobalResult: "ko"}, "ko"},
		{"warned", &runbot.Build{GlobalState: "done", GlobalResult: "warn"}, "warn"},
		{"done without result", &runbot.Build{GlobalState: "done"}, "done"},
		{"running", &runbot.Build{GlobalState: "testing"}, "testing"},
		{"waiting", &runbot.Build{GlobalState: "waiting"}, "waiting"},
		{"unknown", &runbot.Build{}, "unknown"},
		// A failure already known while later steps still run must stay visible.
		{"failing while still running", &runbot.Build{GlobalState: "testing", GlobalResult: "ko"}, "ko (testing)"},
		{"killed while running", &runbot.Build{GlobalState: "running", GlobalResult: "killed"}, "killed (running)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resultLabel(tc.build); got != tc.want {
				t.Errorf("resultLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSiblingsOf(t *testing.T) {
	prs := []runbot.PullRequest{
		{Repo: "odoo/odoo", Number: 286248},
		{Repo: "odoo/enterprise", Number: 130652},
		{Repo: "odoo/design-themes", Number: 1343},
	}
	got := siblingsOf(prs, "enterprise", 130652) // bare name must normalize
	if len(got) != 2 {
		t.Fatalf("got %v, want the two other PRs", got)
	}
	for _, s := range got {
		if s == "odoo/enterprise#130652" {
			t.Error("the requested PR was listed as its own sibling")
		}
	}
}

func TestSplitByPRs(t *testing.T) {
	in := []runbot.BundleSummary{
		{Bundle: runbot.Bundle{Name: "has-pr", PRs: []runbot.PullRequest{{Repo: "odoo/odoo", Number: 1}}}},
		{Bundle: runbot.Bundle{Name: "branch-only"}},
		{Bundle: runbot.Bundle{Name: "also-pr", PRs: []runbot.PullRequest{{Repo: "odoo/enterprise", Number: 2}}}},
	}
	withPRs, branchOnly := splitByPRs(in)
	if len(withPRs) != 2 {
		t.Errorf("got %d with PRs, want 2", len(withPRs))
	}
	if branchOnly != 1 {
		t.Errorf("got %d branch-only, want 1", branchOnly)
	}
}

func TestLimitOf(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{0, defaultPRLimit}, {-5, defaultPRLimit}, {3, 3}} {
		if got := limitOf(tc.in); got != tc.want {
			t.Errorf("limitOf(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// A summary whose batch could not be read must say so rather than present its
// condensed listing state as if it were complete.
func TestRenderMyPRsMarksPartial(t *testing.T) {
	window := []runbot.BundleSummary{{
		Bundle:  runbot.Bundle{Name: "b", PRs: []runbot.PullRequest{{Repo: "odoo/odoo", Number: 7}}},
		BatchID: 99, Overall: runbot.StatePending, Partial: true,
	}}
	out := renderMyPRs(window, 1, 0, true, "who", myPRsArgs{})
	if !strings.Contains(out, "state may be incomplete") {
		t.Errorf("partial summary not flagged:\n%s", out)
	}
}

func TestRenderMyPRsOnlyFailingWhenNoneFail(t *testing.T) {
	window := []runbot.BundleSummary{{
		Bundle:  runbot.Bundle{Name: "b", PRs: []runbot.PullRequest{{Repo: "odoo/odoo", Number: 7}}},
		BatchID: 99, Overall: runbot.StateSuccess,
	}}
	out := renderMyPRs(window, 1, 0, true, "who", myPRsArgs{OnlyFailing: true})
	if !strings.Contains(out, "Nothing is failing") {
		t.Errorf("expected a clear 'nothing failing' message:\n%s", out)
	}
}

// A capped search must say the total is a floor, never present it as a count.
func TestRenderMyPRsMarksCappedSearch(t *testing.T) {
	window := []runbot.BundleSummary{{
		Bundle:  runbot.Bundle{Name: "b", PRs: []runbot.PullRequest{{Repo: "odoo/odoo", Number: 7}}},
		BatchID: 99, Overall: runbot.StateSuccess,
	}}
	out := renderMyPRs(window, 40, 0, false, "who", myPRsArgs{})
	if !strings.Contains(out, "40+ found") {
		t.Errorf("capped total not marked as a floor:\n%s", out)
	}
	if !strings.Contains(out, "capped the search") {
		t.Errorf("capped search not explained:\n%s", out)
	}

	full := renderMyPRs(window, 40, 0, true, "who", myPRsArgs{})
	if strings.Contains(full, "40+ found") || strings.Contains(full, "capped the search") {
		t.Errorf("complete search wrongly marked as capped:\n%s", full)
	}
}

func TestSearchRowsForWidensWithLimit(t *testing.T) {
	if got := searchRowsFor(0); got != runbot.DefaultSearchRows {
		t.Errorf("default rows = %d, want %d", got, runbot.DefaultSearchRows)
	}
	// Raising the report limit must widen the search behind it, otherwise a
	// caller asking for more results silently gets the same truncated set.
	big := searchRowsFor(200)
	if big <= runbot.DefaultSearchRows {
		t.Errorf("searchRowsFor(200) = %d, want more than %d", big, runbot.DefaultSearchRows)
	}
}
