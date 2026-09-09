package runbot

import (
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func loadDoc(t *testing.T, name string) *goquery.Document {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	return doc
}

func TestParseBuildsPassing(t *testing.T) {
	builds := parseBuilds(loadDoc(t, "build_124585551.html").Selection, DefaultBaseURL)
	if len(builds) != 16 {
		t.Fatalf("got %d builds, want 16", len(builds))
	}

	root := builds[0]
	if root.ID != 124585551 {
		t.Errorf("root id = %d, want 124585551", root.ID)
	}
	if root.Host != "runbot297.odoo.com" {
		t.Errorf("host = %q", root.Host)
	}
	if root.Dest != "124585551-master" {
		t.Errorf("dest = %q", root.Dest)
	}
	if root.GlobalResult != "ok" || root.GlobalState != "done" {
		t.Errorf("result/state = %q/%q", root.GlobalResult, root.GlobalState)
	}
	if root.Failed() {
		t.Error("Failed() true for an ok build")
	}
	if !root.Done() {
		t.Error("Done() false for a done build")
	}
	if got := root.LogSteps; len(got) != 1 || got[0] != "install_all" {
		t.Errorf("log steps = %v", got)
	}
	if root.BundleID != 508663 {
		t.Errorf("bundle id = %d, want 508663", root.BundleID)
	}
	if want := "http://runbot297.odoo.com/runbot/static/build/124585551-master/logs/install_all.txt"; root.LogURL("install_all") != want {
		t.Errorf("LogURL = %q, want %q", root.LogURL("install_all"), want)
	}

	// Descendants run on their own hosts, so log URLs must use each build's own
	// host rather than the root's.
	var multiStep bool
	for _, b := range builds[1:] {
		if b.Host == "" {
			t.Errorf("build %d has no host", b.ID)
		}
		if len(b.LogSteps) > 1 {
			multiStep = true
		}
	}
	if !multiStep {
		t.Error("expected at least one descendant with multiple log steps")
	}
}

func TestParseBuildsFailing(t *testing.T) {
	builds := parseBuilds(loadDoc(t, "build_124592268_ko.html").Selection, DefaultBaseURL)
	if len(builds) != 1 {
		t.Fatalf("got %d builds, want 1 (this build has no descendants)", len(builds))
	}
	b := builds[0]
	if b.ID != 124592268 {
		t.Errorf("id = %d", b.ID)
	}
	if b.GlobalResult != "ko" {
		t.Errorf("result = %q, want ko", b.GlobalResult)
	}
	if !b.Failed() {
		t.Error("Failed() false for a ko build")
	}
	if got := b.LogSteps; len(got) != 1 || got[0] != "check_semgrep_security" {
		t.Errorf("log steps = %v", got)
	}
}

func TestParseBuildsNone(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<html><body><p>nope</p></body></html>"))
	if err != nil {
		t.Fatal(err)
	}
	if got := parseBuilds(doc.Selection, DefaultBaseURL); len(got) != 0 {
		t.Errorf("got %d builds from a page with none", len(got))
	}
}

func TestParseLogList(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{`["install_all"]`, []string{"install_all"}},
		{`["restore", "start_post_install_tests"]`, []string{"restore", "start_post_install_tests"}},
		{``, nil},
		{`not json`, nil},
	} {
		got := parseLogList(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("parseLogList(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parseLogList(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestBuildFailed(t *testing.T) {
	for _, tc := range []struct {
		result string
		want   bool
	}{
		{"ok", false}, {"warn", false}, {"", false}, {"skipped", false},
		{"ko", true}, {"killed", true}, {"manually_killed", true},
	} {
		if got := (Build{GlobalResult: tc.result}).Failed(); got != tc.want {
			t.Errorf("Failed(%q) = %v, want %v", tc.result, got, tc.want)
		}
	}
}
