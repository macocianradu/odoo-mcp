package runbot

import "testing"

func TestParsePRLinksCrossRepo(t *testing.T) {
	prs := parsePRLinks(loadDoc(t, "bundle_508663.html").Selection)
	want := map[string]int{
		"odoo/odoo":          286248,
		"odoo/enterprise":    130652,
		"odoo/design-themes": 1343,
	}
	if len(prs) != len(want) {
		t.Fatalf("got %d PRs, want %d: %+v", len(prs), len(want), prs)
	}
	for _, pr := range prs {
		n, ok := want[pr.Repo]
		if !ok {
			t.Errorf("unexpected repo %q", pr.Repo)
			continue
		}
		if pr.Number != n {
			t.Errorf("%s: number = %d, want %d", pr.Repo, pr.Number, n)
		}
		if pr.URL == "" {
			t.Errorf("%s: empty URL", pr.Repo)
		}
	}
}

func TestBundleName(t *testing.T) {
	if got, want := bundleName(loadDoc(t, "bundle_508663.html")), "master-oi-custom-backend-jula"; got != want {
		t.Errorf("bundleName = %q, want %q", got, want)
	}
}

func TestParseBatchIDsNewestFirst(t *testing.T) {
	ids := parseBatchIDs(loadDoc(t, "bundle_508663.html"))
	if len(ids) < 2 {
		t.Fatalf("got %d batch ids, want several", len(ids))
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] <= ids[i] {
			t.Fatalf("batch ids not sorted newest first: %v", ids)
		}
	}
}

func TestParseBundleIDsFromSearch(t *testing.T) {
	ids := parseBundleIDs(loadDoc(t, "search_286248.html"))
	if len(ids) != 1 || ids[0] != 508663 {
		t.Fatalf("got %v, want [508663]", ids)
	}
}

func TestParseSlotsIncludesCommunityAndEnterprise(t *testing.T) {
	slots := parseSlots(loadDoc(t, "batch_2754387.html").Selection, DefaultBaseURL)
	if len(slots) == 0 {
		t.Fatal("no slots parsed")
	}
	byTrigger := map[string]Slot{}
	for _, s := range slots {
		byTrigger[s.Trigger] = s
	}
	for _, name := range []string{"Community Run", "Enterprise Run"} {
		s, ok := byTrigger[name]
		if !ok {
			t.Errorf("missing trigger %q; got %v", name, keysOf(byTrigger))
			continue
		}
		if s.Build == nil {
			t.Errorf("%q has no build", name)
			continue
		}
		if s.Build.ID == 0 || s.Build.Host == "" {
			t.Errorf("%q build incomplete: %+v", name, s.Build)
		}
	}
}

func TestNormalizeRepo(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"enterprise", "odoo/enterprise"},
		{"odoo", "odoo/odoo"},
		{"odoo/odoo", "odoo/odoo"},
		{"odoo/enterprise", "odoo/enterprise"},
		{" odoo/enterprise/ ", "odoo/enterprise"},
		{"", ""},
	} {
		if got := NormalizeRepo(tc.in); got != tc.want {
			t.Errorf("NormalizeRepo(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
