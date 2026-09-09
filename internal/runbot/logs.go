package runbot

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// LogFile is one file in a build's log directory.
type LogFile struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size_bytes"`
}

// LogsBaseURL returns the directory that holds a build's log files.
//
// These are served directly by the build host's nginx over plain http with
// autoindex enabled, independently of the runbot Odoo frontend, which is why
// they remain readable without any credentials.
func LogsBaseURL(host, dest string) string {
	return fmt.Sprintf("http://%s/runbot/static/build/%s/logs/", host, dest)
}

// nginx autoindex renders "<a href="name">name</a>   date   size".
var autoindexSize = regexp.MustCompile(`</a>\s+\S+\s+\S+\s+(\d+)`)

// ListLogs returns the log files available for a build.
//
// A build that never ran on a host (host or dest empty) has no log directory;
// callers get an empty slice rather than an error, since that is a normal state
// for skipped or instantly-failed builds rather than a fault.
func (c *Client) ListLogs(ctx context.Context, host, dest string) ([]LogFile, error) {
	if host == "" || dest == "" {
		return nil, nil
	}
	base := LogsBaseURL(host, dest)
	body, _, err := c.fetch(ctx, base)
	if err != nil {
		if NotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("runbot: parsing log index at %s: %w", base, err)
	}

	// Sizes are plain text siblings of each link, so pair them up by matching
	// the raw markup line-wise rather than walking text nodes.
	sizes := map[string]int64{}
	for _, line := range strings.Split(string(body), "\n") {
		href := hrefOf(line)
		if href == "" {
			continue
		}
		if m := autoindexSize.FindStringSubmatch(line); m != nil {
			n, _ := strconv.ParseInt(m[1], 10, 64)
			sizes[href] = n
		}
	}

	var files []LogFile
	doc.Find("pre a").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || href == "" || strings.HasPrefix(href, "..") || strings.HasSuffix(href, "/") {
			return
		}
		files = append(files, LogFile{Name: href, URL: base + href, Size: sizes[href]})
	})
	return files, nil
}

var hrefRe = regexp.MustCompile(`<a href="([^"]+)"`)

func hrefOf(line string) string {
	if m := hrefRe.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

// FetchLog returns the full contents of a log file as lines.
func (c *Client) FetchLog(ctx context.Context, url string) ([]string, error) {
	body, _, err := c.fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	return strings.Split(strings.TrimRight(text, "\n"), "\n"), nil
}
