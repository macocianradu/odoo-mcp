package runbot

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ParseError reports that a page loaded but did not contain the structure we
// expect. It names the selector so a runbot frontend change produces an
// actionable message instead of an empty result.
type ParseError struct {
	URL      string
	Selector string
	Detail   string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("runbot: %s did not contain %s (%s); the runbot frontend may have changed", e.URL, e.Selector, e.Detail)
}

// buildSelector matches the custom element runbot renders for each build.
// It is a web component whose data-* attributes carry raw ORM values.
const buildSelector = "build-options-dropdown[data-id]"

// Build is one runbot build.
//
// Fields come from the data-* attributes runbot renders onto its custom
// <build> element for its own JavaScript. Those carry raw ORM values
// (global_result, host, dest, log_list), which makes them markedly more stable
// to read than the surrounding presentational markup.
type Build struct {
	ID           int      `json:"id"`
	Dest         string   `json:"dest"`
	Host         string   `json:"host"`
	GlobalState  string   `json:"global_state"`
	GlobalResult string   `json:"global_result"`
	LocalState   string   `json:"local_state"`
	Description  string   `json:"description,omitempty"`
	LogSteps     []string `json:"log_steps,omitempty"`
	BundleID     int      `json:"bundle_id,omitempty"`
	TriggerID    int      `json:"trigger_id,omitempty"`
	URL          string   `json:"url"`
}

// Failed reports whether this build carries a failing result.
func (b Build) Failed() bool {
	switch b.GlobalResult {
	case "ko", "killed", "manually_killed":
		return true
	}
	return false
}

// Done reports whether the build has finished, so callers can distinguish
// "not failing" from "not finished yet".
func (b Build) Done() bool { return b.GlobalState == "done" }

// LogURL returns the URL of one log step for this build.
func (b Build) LogURL(step string) string {
	return LogsBaseURL(b.Host, b.Dest) + step + ".txt"
}

// BuildPage is a build together with the descendant builds runbot renders
// alongside it. Failures usually live in a descendant rather than in the build
// a CI status points at.
type BuildPage struct {
	Build       Build   `json:"build"`
	Descendants []Build `json:"descendants,omitempty"`
}

// Build fetches and parses a build page.
func (c *Client) Build(ctx context.Context, id int) (*BuildPage, error) {
	url := c.buildURL(id)
	doc, finalURL, err := c.fetchDoc(ctx, url)
	if err != nil {
		return nil, err
	}

	builds := parseBuilds(doc.Selection, c.BaseURL)
	if len(builds) == 0 {
		return nil, &ParseError{URL: finalURL, Selector: buildSelector, Detail: "no build elements"}
	}

	page := &BuildPage{}
	var found bool
	for _, b := range builds {
		switch {
		case b.ID == id && !found:
			page.Build = b
			found = true
		default:
			page.Descendants = append(page.Descendants, b)
		}
	}
	if !found {
		// The page rendered builds but not the one requested; treat the first
		// as the subject rather than failing, and keep the rest as context.
		page.Build = builds[0]
		page.Descendants = builds[1:]
	}
	return page, nil
}

// parseBuilds reads every <build> element on a page, de-duplicating by id since
// runbot renders the same build more than once (button plus dropdown).
func parseBuilds(sel *goquery.Selection, baseURL string) []Build {
	var out []Build
	seen := map[int]bool{}

	sel.Find(buildSelector).Each(func(_ int, s *goquery.Selection) {
		idStr, ok := s.Attr("data-id")
		if !ok {
			return
		}
		id, err := strconv.Atoi(strings.TrimSpace(idStr))
		if err != nil || seen[id] {
			return
		}
		seen[id] = true

		b := Build{
			ID:           id,
			Dest:         attr(s, "data-dest"),
			Host:         attr(s, "data-host"),
			GlobalState:  attr(s, "data-global_state"),
			GlobalResult: attr(s, "data-global_result"),
			LocalState:   attr(s, "data-local_state"),
			Description:  attr(s, "data-description"),
			LogSteps:     parseLogList(attr(s, "data-log_list")),
			BundleID:     atoi(attr(s, "data-bundle_id")),
			TriggerID:    atoi(attr(s, "data-trigger_id")),
			URL:          fmt.Sprintf("%s/runbot/build/%d", baseURL, id),
		}
		out = append(out, b)
	})
	return out
}

// parseLogList reads data-log_list, a JSON array of step names.
func parseLogList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var steps []string
	if err := json.Unmarshal([]byte(raw), &steps); err != nil {
		return nil
	}
	return steps
}

func attr(s *goquery.Selection, name string) string {
	v, _ := s.Attr(name)
	return strings.TrimSpace(v)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
