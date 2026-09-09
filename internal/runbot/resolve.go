package runbot

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// PullRequest is a GitHub pull request linked to a runbot bundle.
type PullRequest struct {
	Repo   string `json:"repo"` // "odoo/odoo", "odoo/enterprise", ...
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// Bundle groups the branches that runbot tests together.
//
// A bundle spans repositories: a feature branch pushed to odoo/odoo,
// odoo/enterprise and design-themes under the same name is one bundle with one
// set of builds. This cross-repo grouping is the view GitHub cannot give you.
type Bundle struct {
	ID      int           `json:"id"`
	Name    string        `json:"name"`
	URL     string        `json:"url"`
	PRs     []PullRequest `json:"pull_requests,omitempty"`
	Batches []int         `json:"batch_ids,omitempty"` // newest first
}

// Slot is one trigger's build within a batch, e.g. "Community Run".
type Slot struct {
	Trigger string `json:"trigger"`
	Build   *Build `json:"build,omitempty"`
	// BatchID is the batch the slot belongs to. Search results list several
	// batches per bundle, so slots must be attributable to one of them.
	BatchID int `json:"batch_id,omitempty"`
}

// Batch is one run of a bundle: a set of commits tested across every trigger.
type Batch struct {
	ID    int    `json:"id"`
	URL   string `json:"url"`
	Slots []Slot `json:"slots"`
}

var prLinkRe = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/pull/(\d+)`)

// NormalizeRepo expands a bare repo name to owner/name, defaulting to the odoo
// organisation so callers may pass "enterprise" as well as "odoo/enterprise".
func NormalizeRepo(repo string) string {
	repo = strings.TrimSuffix(strings.TrimSpace(repo), "/")
	if repo == "" {
		return ""
	}
	if !strings.Contains(repo, "/") {
		return "odoo/" + repo
	}
	return repo
}

// Bundle fetches and parses a bundle page.
func (c *Client) Bundle(ctx context.Context, id int) (*Bundle, error) {
	doc, finalURL, err := c.fetchDoc(ctx, c.bundleURL(id))
	if err != nil {
		return nil, err
	}
	b := &Bundle{ID: id, URL: finalURL, Name: bundleName(doc)}
	b.PRs = parsePRLinks(doc.Selection)
	b.Batches = parseBatchIDs(doc)
	if b.Name == "" && len(b.PRs) == 0 && len(b.Batches) == 0 {
		return nil, &ParseError{URL: finalURL, Selector: "title / a[href*=github.com] / a[href^=/runbot/batch/]", Detail: "no bundle content"}
	}
	return b, nil
}

// bundleName reads the page title, rendered as "Bundle <name>".
func bundleName(doc *goquery.Document) string {
	t := strings.TrimSpace(doc.Find("title").First().Text())
	return strings.TrimSpace(strings.TrimPrefix(t, "Bundle "))
}

func parsePRLinks(sel *goquery.Selection) []PullRequest {
	var out []PullRequest
	seen := map[string]bool{}
	sel.Find(`a[href*="/pull/"]`).Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		m := prLinkRe.FindStringSubmatch(strings.TrimSpace(href))
		if m == nil {
			return
		}
		n, err := strconv.Atoi(m[3])
		if err != nil {
			return
		}
		repo := m[1] + "/" + m[2]
		key := fmt.Sprintf("%s#%d", repo, n)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, PullRequest{Repo: repo, Number: n, URL: m[0]})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Repo < out[j].Repo })
	return out
}

var batchHrefRe = regexp.MustCompile(`^/runbot/batch/(\d+)`)

// parseBatchIDs collects batch ids, newest first. Ids are monotonically
// increasing, so sorting by id is more reliable than trusting page order.
func parseBatchIDs(doc *goquery.Document) []int {
	seen := map[int]bool{}
	var ids []int
	doc.Find(`a[href^="/runbot/batch/"]`).Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		m := batchHrefRe.FindStringSubmatch(href)
		if m == nil {
			return
		}
		id, err := strconv.Atoi(m[1])
		if err != nil || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	})
	sort.Sort(sort.Reverse(sort.IntSlice(ids)))
	return ids
}

// Batch fetches a batch and the per-trigger builds it contains.
func (c *Client) Batch(ctx context.Context, id int) (*Batch, error) {
	doc, finalURL, err := c.fetchDoc(ctx, c.batchURL(id))
	if err != nil {
		return nil, err
	}
	batch := &Batch{ID: id, URL: finalURL, Slots: parseSlots(doc.Selection, c.BaseURL)}

	if len(batch.Slots) == 0 {
		return nil, &ParseError{URL: finalURL, Selector: ".slot_button_group a.slot_name", Detail: "no trigger slots"}
	}
	return batch, nil
}

// FindBundleForPR resolves a pull request to the bundle runbot tests it in.
//
// runbot stores a PR as a branch whose name is the PR number, so its frontend
// search matches on the bare number. Candidate bundles are then confirmed by
// checking that they actually link the requested repo and number, since a bare
// number can match more than one bundle.
func (c *Client) FindBundleForPR(ctx context.Context, repo string, number int) (*Bundle, error) {
	repo = NormalizeRepo(repo)
	searchURL := fmt.Sprintf("%s/runbot/%s/search/%d", c.BaseURL, url.PathEscape(c.Project), number)
	doc, finalURL, err := c.fetchDoc(ctx, searchURL)
	if err != nil {
		return nil, err
	}

	ids := parseBundleIDs(doc)
	if len(ids) == 0 {
		return nil, fmt.Errorf("runbot: no bundle found for %s#%d (searched %s); the PR may be too new to have been picked up, or closed", repo, number, finalURL)
	}

	var firstErr error
	for _, id := range ids {
		b, err := c.Bundle(ctx, id)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, pr := range b.PRs {
			if pr.Number == number && strings.EqualFold(pr.Repo, repo) {
				return b, nil
			}
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, fmt.Errorf("runbot: search for %d matched %d bundle(s), none linking %s#%d", number, len(ids), repo, number)
}

var bundleHrefRe = regexp.MustCompile(`^/runbot/bundle/(\d+)`)

func parseBundleIDs(doc *goquery.Document) []int {
	seen := map[int]bool{}
	var ids []int
	doc.Find(`a[href^="/runbot/bundle/"]`).Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		m := bundleHrefRe.FindStringSubmatch(href)
		if m == nil {
			return
		}
		id, err := strconv.Atoi(m[1])
		if err != nil || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	})
	return ids
}

// parseSlots reads the per-trigger build slots of a batch page. Each slot is a
// .slot_button_group holding the trigger name and that trigger's build.
func parseSlots(sel *goquery.Selection, baseURL string) []Slot {
	var slots []Slot
	sel.Find(".slot_button_group").Each(func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("a.slot_name").First().Text())
		if name == "" {
			return
		}
		slot := Slot{Trigger: name, BatchID: slotBatchID(s)}
		if builds := parseBuilds(s, baseURL); len(builds) > 0 {
			b := builds[0]
			slot.Build = &b
		}
		slots = append(slots, slot)
	})
	return slots
}

var slotBatchRe = regexp.MustCompile(`^/runbot/batch/(\d+)/build/\d+`)

// slotBatchID reads the batch a slot belongs to from its build link.
func slotBatchID(s *goquery.Selection) int {
	href, _ := s.Find("a.slot_name").First().Attr("href")
	if m := slotBatchRe.FindStringSubmatch(href); m != nil {
		return atoi(m[1])
	}
	return 0
}
