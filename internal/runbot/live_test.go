//go:build live

// Live smoke tests against the real runbot. These exist so that a change to
// runbot's frontend surfaces as an explicit failure rather than as tools that
// silently return nothing. Run with: go test -tags=live ./...
package runbot

import (
	"context"
	"testing"
	"time"
)

// skipIfGone lets a live test skip rather than fail when runbot has garbage
// collected a pinned fixture build. A vanished build is expected over time; a
// parse failure on a build that still exists is the signal worth reporting.
func skipIfGone(t *testing.T, err error) {
	t.Helper()
	if NotFound(err) {
		t.Skipf("fixture no longer on runbot (garbage collected): %v", err)
	}
}

func liveClient(t *testing.T) (*Client, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return New("", ""), ctx
}

func TestLiveListLogs(t *testing.T) {
	c, ctx := liveClient(t)
	files, err := c.ListLogs(ctx, "runbot317.odoo.com", "124592268-master")
	if err != nil {
		skipIfGone(t, err)
		t.Fatalf("ListLogs: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no log files listed; autoindex format may have changed")
	}
	var found bool
	for _, f := range files {
		t.Logf("%s (%d bytes) %s", f.Name, f.Size, f.URL)
		if f.Name == "check_semgrep_security.txt" {
			found = true
			if f.Size == 0 {
				t.Error("size not parsed from autoindex")
			}
		}
	}
	if !found {
		t.Error("expected check_semgrep_security.txt in listing")
	}
}

func TestLiveFetchLog(t *testing.T) {
	c, ctx := liveClient(t)
	lines, err := c.FetchLog(ctx, LogsBaseURL("runbot317.odoo.com", "124592268-master")+"check_semgrep_security.txt")
	if err != nil {
		skipIfGone(t, err)
		t.Fatalf("FetchLog: %v", err)
	}
	if len(lines) < 5 {
		t.Fatalf("expected a multi-line log, got %d lines", len(lines))
	}
	t.Logf("%d lines, last: %q", len(lines), lines[len(lines)-1])
}

func TestLiveBuild(t *testing.T) {
	c, ctx := liveClient(t)
	page, err := c.Build(ctx, 124585551)
	if err != nil {
		skipIfGone(t, err)
		t.Fatalf("Build: %v", err)
	}
	if page.Build.ID != 124585551 {
		t.Errorf("id = %d", page.Build.ID)
	}
	if page.Build.Host == "" || page.Build.Dest == "" {
		t.Errorf("missing host/dest: %+v", page.Build)
	}
	t.Logf("build %d result=%s state=%s host=%s steps=%v, %d descendants",
		page.Build.ID, page.Build.GlobalResult, page.Build.GlobalState,
		page.Build.Host, page.Build.LogSteps, len(page.Descendants))
}

func TestLiveBatch(t *testing.T) {
	c, ctx := liveClient(t)
	batch, err := c.Batch(ctx, 2754387)
	if err != nil {
		skipIfGone(t, err)
		t.Fatalf("Batch: %v", err)
	}
	for _, s := range batch.Slots {
		id := 0
		if s.Build != nil {
			id = s.Build.ID
		}
		t.Logf("  %-24s build=%d", s.Trigger, id)
	}
}

// TestLiveFindBundleForPR covers both a community and an enterprise pull
// request, since enterprise coverage is the whole point of the server and its
// runbot data is public even though the GitHub repo is not.
func TestLiveFindBundleForPR(t *testing.T) {
	for _, tc := range []struct {
		repo string
		pr   int
	}{
		{"odoo/odoo", 286248},
		{"odoo/enterprise", 130652},
		{"enterprise", 130652}, // bare name must normalize
	} {
		t.Run(tc.repo, func(t *testing.T) {
			c, ctx := liveClient(t)
			b, err := c.FindBundleForPR(ctx, tc.repo, tc.pr)
			if err != nil {
				skipIfGone(t, err)
				t.Fatalf("FindBundleForPR(%s#%d): %v", tc.repo, tc.pr, err)
			}
			t.Logf("bundle %d %q, %d PRs, %d batches (latest %v)", b.ID, b.Name, len(b.PRs), len(b.Batches), firstOf(b.Batches))
			for _, pr := range b.PRs {
				t.Logf("    %s#%d", pr.Repo, pr.Number)
			}
			if len(b.Batches) == 0 {
				t.Error("no batches on bundle")
			}
		})
	}
}

func firstOf(xs []int) any {
	if len(xs) == 0 {
		return nil
	}
	return xs[0]
}

func TestLivePRStatus(t *testing.T) {
	c, ctx := liveClient(t)
	st, err := c.PRStatus(ctx, "odoo/enterprise", 130652)
	if err != nil {
		skipIfGone(t, err)
		t.Fatalf("PRStatus: %v", err)
	}
	t.Logf("bundle %q overall=%s batch=%d slots=%d failing=%d",
		st.Bundle.Name, st.Overall, st.Batch.ID, len(st.Batch.Slots), len(st.Failing))
	for _, s := range st.Failing {
		t.Logf("   FAILING %s -> build %d", s.Trigger, s.Build.ID)
	}
	if st.Overall == StateUnknown {
		t.Error("overall state unknown")
	}
	if st.PR.Number != 130652 {
		t.Errorf("PR not matched back: %+v", st.PR)
	}
}

func TestLiveDiagnose(t *testing.T) {
	// 124592268: semgrep failure whose log contains no ERROR line.
	// 124593755: an eslint test failure with a real traceback.
	for _, id := range []int{124592268, 124593755} {
		c, ctx := liveClient(t)
		d, err := c.Diagnose(ctx, id, DiagnoseOptions{})
		if err != nil {
			skipIfGone(t, err)
			t.Fatalf("Diagnose(%d): %v", id, err)
		}
		t.Logf("=== build %d result=%s note=%q failing=%d",
			d.Build.ID, d.Build.GlobalResult, d.Note, len(d.FailingBuilds))
		for _, fb := range d.FailingBuilds {
			for _, ex := range fb.Logs {
				t.Logf("  build %d step %s: %d lines, %d snippet(s) note=%q",
					fb.Build.ID, ex.Step, ex.TotalLines, len(ex.Snippets), ex.Note)
				if len(ex.Snippets) > 0 {
					for _, l := range ex.Snippets[0].Lines {
						if len(l) > 100 {
							l = l[:100] + "…"
						}
						t.Logf("      | %s", l)
					}
				}
			}
			for _, e := range fb.FetchErrors {
				t.Logf("  fetch error: %s", e)
			}
		}
		if len(d.FailingBuilds) == 0 {
			t.Errorf("build %d: no failing builds found", id)
		}
	}
}

// Runbot's search returns only 40 rows unless a limit is given, and says
// nothing about having truncated. This guards the fix for that.
func TestLiveSearchBundlesRespectsLimit(t *testing.T) {
	c, ctx := liveClient(t)

	small, complete, err := c.SearchBundles(ctx, "admac", 10)
	if err != nil {
		skipIfGone(t, err)
		t.Fatalf("SearchBundles(10): %v", err)
	}
	if len(small) != 10 {
		t.Errorf("asked for 10 rows, got %d", len(small))
	}
	if complete {
		t.Error("a search filled to its cap must not report itself complete")
	}

	full, complete, err := c.SearchBundles(ctx, "admac", 0)
	if err != nil {
		t.Fatalf("SearchBundles(default): %v", err)
	}
	t.Logf("default search returned %d bundles, complete=%v", len(full), complete)
	if len(full) <= 40 {
		t.Errorf("got %d bundles; the un-limited search returns 40, so the limit is not being applied", len(full))
	}
	if !complete {
		t.Errorf("default of %d rows still capped at %d results", DefaultSearchRows, len(full))
	}
}
