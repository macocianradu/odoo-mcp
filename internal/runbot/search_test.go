package runbot

import "testing"

// A search row lists several batches; only the newest describes current state.
func TestParseBundleRowUsesLatestBatch(t *testing.T) {
	row := loadDoc(t, "search_286248.html").Find(".bundle_row").First()
	if row.Length() == 0 {
		t.Fatal("no .bundle_row in fixture")
	}

	sum, ok := parseBundleRow(row, DefaultBaseURL)
	if !ok {
		t.Fatal("row did not parse")
	}
	if sum.Bundle.ID != 508663 {
		t.Errorf("bundle id = %d, want 508663", sum.Bundle.ID)
	}
	if sum.Bundle.Name != "master-oi-custom-backend-jula" {
		t.Errorf("bundle name = %q", sum.Bundle.Name)
	}
	if len(sum.Bundle.PRs) != 3 {
		t.Errorf("got %d PRs, want 3: %+v", len(sum.Bundle.PRs), sum.Bundle.PRs)
	}

	// The fixture holds batches 2754298/2754327/2754387/2754637.
	if sum.BatchID != 2754637 {
		t.Errorf("latest batch = %d, want 2754637", sum.BatchID)
	}
	if len(sum.Slots) == 0 {
		t.Fatal("no slots for the latest batch")
	}
	for _, s := range sum.Slots {
		if s.BatchID != sum.BatchID {
			t.Errorf("slot %q belongs to batch %d, not the latest %d", s.Trigger, s.BatchID, sum.BatchID)
		}
	}
	if sum.Overall == StateUnknown {
		t.Error("overall state not computed")
	}
}

func TestSortBundlesNewestFirst(t *testing.T) {
	s := []BundleSummary{{BatchID: 10}, {BatchID: 300}, {BatchID: 50}}
	SortBundles(s)
	if s[0].BatchID != 300 || s[2].BatchID != 10 {
		t.Errorf("sorted order = %d,%d,%d", s[0].BatchID, s[1].BatchID, s[2].BatchID)
	}
}

func TestBundleSummaryFailing(t *testing.T) {
	s := BundleSummary{Slots: []Slot{
		{Trigger: "ok one", Build: &Build{GlobalState: "done", GlobalResult: "ok"}},
		{Trigger: "bad", Build: &Build{GlobalState: "done", GlobalResult: "ko"}},
		{Trigger: "no build"},
	}}
	failing := s.Failing()
	if len(failing) != 1 || failing[0].Trigger != "bad" {
		t.Errorf("Failing() = %+v", failing)
	}
}
