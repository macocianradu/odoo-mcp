package runbot

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
)

// BundleSummary is one row of a runbot search result: a bundle, the pull
// requests it tests, and the state of its most recent batch.
type BundleSummary struct {
	Bundle  Bundle `json:"bundle"`
	BatchID int    `json:"latest_batch_id,omitempty"`
	Slots   []Slot `json:"slots,omitempty"`
	Overall string `json:"overall"`
	// Partial marks a summary whose state still comes from the search listing.
	// That listing is condensed — it renders only a couple of triggers per
	// batch — so it can miss failures entirely and must not be reported as if
	// it were the whole picture. ResolveStates clears this.
	Partial bool `json:"partial,omitempty"`
}

// Failing returns the slots of the latest batch that failed.
func (s BundleSummary) Failing() []Slot {
	var out []Slot
	for _, slot := range s.Slots {
		if slot.Build != nil && slot.Build.Failed() {
			out = append(out, slot)
		}
	}
	return out
}

// DefaultSearchRows is how many bundles a search asks runbot for when the
// caller has no stronger opinion. Runbot's search returns only 40 rows unless a
// limit is given, which silently hides older matches.
const DefaultSearchRows = 250

// SearchBundles runs runbot's bundle search and summarises each result.
//
// One request is enough: runbot renders every matching bundle as a row that
// already carries its pull requests and its recent builds, so no per-bundle
// follow-up is needed.
//
// rows caps how many bundles runbot returns. This must be passed explicitly:
// the search route defaults to 40 rows and gives no indication that it has
// truncated, so a caller that omits it silently loses older matches.
//
// complete reports whether the whole result set was seen. It is false when
// runbot returned exactly the number of rows asked for, since more may exist
// beyond the cap.
//
// Odoo branch names conventionally end in the author's trigram, which makes a
// trigram a practical way to find one person's work — but it is a substring
// match on the bundle name, not an author field, so unrelated bundles can match.
func (c *Client) SearchBundles(ctx context.Context, query string, rows int) (out []BundleSummary, complete bool, err error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, false, fmt.Errorf("runbot: search needs a query")
	}
	if rows <= 0 {
		rows = DefaultSearchRows
	}

	searchURL := fmt.Sprintf("%s/runbot/%s/search/%s?limit=%d",
		c.BaseURL, url.PathEscape(c.Project), url.PathEscape(query), rows)
	doc, finalURL, err := c.fetchDoc(ctx, searchURL)
	if err != nil {
		return nil, false, err
	}

	sel := doc.Find(".bundle_row")
	if sel.Length() == 0 {
		return nil, true, nil // a search with no hits is an answer, not a fault
	}

	sel.Each(func(_ int, row *goquery.Selection) {
		if s, ok := parseBundleRow(row, c.BaseURL); ok {
			out = append(out, s)
		}
	})
	if len(out) == 0 {
		return nil, false, &ParseError{URL: finalURL, Selector: ".bundle_row a[href^=/runbot/bundle/]", Detail: "rows present but none parsed"}
	}
	return out, sel.Length() < rows, nil
}

func parseBundleRow(row *goquery.Selection, baseURL string) (BundleSummary, bool) {
	link := row.Find(`a[href^="/runbot/bundle/"]`).First()
	href, ok := link.Attr("href")
	if !ok {
		return BundleSummary{}, false
	}
	m := bundleHrefRe.FindStringSubmatch(href)
	if m == nil {
		return BundleSummary{}, false
	}

	name := strings.TrimSpace(link.Text())
	if name == "" {
		// The link title reads "View Bundle <name>".
		title, _ := link.Attr("title")
		name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(title), "View Bundle"))
	}

	sum := BundleSummary{
		Bundle: Bundle{
			ID:   atoi(m[1]),
			Name: name,
			URL:  fmt.Sprintf("%s/runbot/bundle/%s", baseURL, m[1]),
			PRs:  parsePRLinks(row),
		},
		Overall: StateUnknown,
	}

	// A row shows several batches; only the newest describes the current state.
	all := parseSlots(row, baseURL)
	for _, slot := range all {
		if slot.BatchID > sum.BatchID {
			sum.BatchID = slot.BatchID
		}
	}
	for _, slot := range all {
		if slot.BatchID == sum.BatchID {
			sum.Slots = append(sum.Slots, slot)
		}
	}
	if len(sum.Slots) > 0 {
		sum.Overall, _ = summarize(sum.Slots)
	}
	sum.Partial = true
	return sum, true
}

// SortBundles orders summaries by latest batch, newest first, so the work a
// person touched most recently comes first.
func SortBundles(s []BundleSummary) {
	sort.SliceStable(s, func(i, j int) bool { return s[i].BatchID > s[j].BatchID })
}

// resolveConcurrency bounds simultaneous batch fetches, to stay light on runbot.
const resolveConcurrency = 6

// ResolveStates replaces each summary's condensed state with its real one by
// reading the batch page.
//
// This is not an optimisation but a correctness requirement: a search row shows
// only a subset of a batch's triggers, so a bundle whose visible trigger is
// still running can read as "pending" while another trigger has already failed.
// A summary whose batch cannot be read keeps its listing state and stays marked
// Partial, so callers can say so rather than present a guess as fact.
func (c *Client) ResolveStates(ctx context.Context, sums []BundleSummary) {
	sem := make(chan struct{}, resolveConcurrency)
	var wg sync.WaitGroup

	for i := range sums {
		if sums[i].BatchID == 0 {
			sums[i].Partial = false // nothing has run; the listing is complete
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			batch, err := c.Batch(ctx, sums[i].BatchID)
			if err != nil {
				return // keep the condensed state, still marked Partial
			}
			sums[i].Slots = batch.Slots
			sums[i].Overall, _ = summarize(batch.Slots)
			sums[i].Partial = false
		}(i)
	}
	wg.Wait()
}
