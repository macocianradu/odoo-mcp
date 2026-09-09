// Package runbot provides a read-only client for Odoo's runbot CI frontend.
//
// Every route it touches is publicly readable: build pages, batch pages, bundle
// pages and the raw log files served by each build host's nginx. No credentials
// are required or accepted. Only actions (rebuild, kill, wake up) are gated
// behind a login, and this package deliberately performs none of them.
package runbot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	// DefaultBaseURL is the public Odoo runbot instance.
	DefaultBaseURL = "https://runbot.odoo.com"
	// DefaultProject is the R&D project slug, which holds odoo, enterprise,
	// design-themes, documentation, upgrade and upgrade-util. The search route
	// is project-scoped, so this has to be configurable.
	DefaultProject = "rd-1"

	userAgent = "odoo-mcp (+https://github.com/odoo/odoo-mcp)"

	// maxFetch caps any single response we will read into memory. Build logs
	// routinely reach tens of megabytes; this is a backstop against a
	// pathological one, not the user-facing truncation limit.
	maxFetch = 32 << 20
)

// Client talks to a runbot frontend over plain HTTP(S).
//
// Log files live on per-build hosts (runbot297.odoo.com and friends) and are
// served over plain http, so requests must not be upgraded to https.
type Client struct {
	BaseURL string
	Project string
	HTTP    *http.Client
}

// New returns a Client, falling back to the public runbot for empty values.
func New(baseURL, project string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if project == "" {
		project = DefaultProject
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Project: project,
		HTTP:    &http.Client{Timeout: 45 * time.Second},
	}
}

// HTTPError reports a non-2xx response, keeping the URL so callers can say
// which request failed rather than surfacing a bare status code.
type HTTPError struct {
	URL    string
	Status int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("runbot: GET %s returned HTTP %d", e.URL, e.Status)
}

// NotFound reports whether err is a 404, which for runbot means the build,
// batch or bundle id does not exist rather than that the service is broken.
func NotFound(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.Status == http.StatusNotFound
}

// fetch performs a GET and returns the body along with the URL actually served,
// which differs from the requested one whenever runbot redirects (bundle ids
// redirect to their slug form).
func (c *Client) fetch(ctx context.Context, rawURL string) (body []byte, finalURL string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("runbot: building request for %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("runbot: GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	finalURL = rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, finalURL, &HTTPError{URL: rawURL, Status: resp.StatusCode}
	}

	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFetch))
	if err != nil {
		return nil, finalURL, fmt.Errorf("runbot: reading %s: %w", rawURL, err)
	}
	return b, finalURL, nil
}

// fetchDoc retrieves an HTML page and parses it.
func (c *Client) fetchDoc(ctx context.Context, rawURL string) (*goquery.Document, string, error) {
	body, finalURL, err := c.fetch(ctx, rawURL)
	if err != nil {
		return nil, finalURL, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, finalURL, fmt.Errorf("runbot: parsing HTML from %s: %w", finalURL, err)
	}
	return doc, finalURL, nil
}

func (c *Client) buildURL(id int) string  { return fmt.Sprintf("%s/runbot/build/%d", c.BaseURL, id) }
func (c *Client) batchURL(id int) string  { return fmt.Sprintf("%s/runbot/batch/%d", c.BaseURL, id) }
func (c *Client) bundleURL(id int) string { return fmt.Sprintf("%s/runbot/bundle/%d", c.BaseURL, id) }
