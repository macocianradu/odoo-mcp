package runbot

import (
	"context"
	"fmt"
	"sync"
)

// BuildFailure is one failing build together with digests of its logs.
type BuildFailure struct {
	Build       Build        `json:"build"`
	Logs        []Extraction `json:"logs,omitempty"`
	FetchErrors []string     `json:"fetch_errors,omitempty"`
}

// Diagnosis explains why a build failed.
type Diagnosis struct {
	Build         Build          `json:"build"`
	FailingBuilds []BuildFailure `json:"failing_builds,omitempty"`
	Note          string         `json:"note,omitempty"`
}

// DiagnoseOptions bounds how much work and output a diagnosis produces.
type DiagnoseOptions struct {
	MaxBuilds        int // failing builds to inspect
	MaxStepsPerBuild int
	Extract          ExtractOptions
}

func (o DiagnoseOptions) withDefaults() DiagnoseOptions {
	if o.MaxBuilds <= 0 {
		o.MaxBuilds = 3
	}
	if o.MaxStepsPerBuild <= 0 {
		o.MaxStepsPerBuild = 4
	}
	return o
}

// Diagnose fetches a build and reports the failures beneath it.
//
// The build a CI status points at is usually a parent whose children hold the
// real failure, so descendants are preferred over the parent when both failed;
// the parent's own result is just an aggregate of theirs.
func (c *Client) Diagnose(ctx context.Context, buildID int, opts DiagnoseOptions) (*Diagnosis, error) {
	opts = opts.withDefaults()

	page, err := c.Build(ctx, buildID)
	if err != nil {
		return nil, err
	}
	d := &Diagnosis{Build: page.Build}

	targets := selectFailing(page, opts.MaxBuilds)
	if len(targets) == 0 {
		switch {
		case !page.Build.Done():
			d.Note = fmt.Sprintf("build %d is still %s; no failure to report yet", page.Build.ID, stateOrUnknown(page.Build.GlobalState))
		case page.Build.Failed():
			d.Note = fmt.Sprintf("build %d failed but wrote no logs, so there is nothing to extract; see %s", page.Build.ID, page.Build.URL)
		default:
			d.Note = fmt.Sprintf("build %d did not fail (result %s)", page.Build.ID, stateOrUnknown(page.Build.GlobalResult))
		}
		return d, nil
	}

	d.FailingBuilds = make([]BuildFailure, len(targets))

	// Each log fetch writes to its own slot, so results stay in step order and
	// need no locking. A log that fails to download is recorded rather than
	// aborting the rest of the report.
	for i, b := range targets {
		d.FailingBuilds[i] = BuildFailure{Build: b}

		steps := b.LogSteps
		if len(steps) > opts.MaxStepsPerBuild {
			steps = steps[len(steps)-opts.MaxStepsPerBuild:] // failures land in the last steps
		}
		logs := make([]Extraction, len(steps))
		errs := make([]string, len(steps))

		var wg sync.WaitGroup

		for j, step := range steps {
			wg.Add(1)
			go func() {
				defer wg.Done()
				url := b.LogURL(step)
				lines, err := c.FetchLog(ctx, url)
				if err != nil {
					errs[j] = err.Error()
					return
				}
				logs[j] = Extract(step, url, lines, opts.Extract)
			}()
		}
		wg.Wait()

		for j := range steps {
			if errs[j] != "" {
				d.FailingBuilds[i].FetchErrors = append(d.FailingBuilds[i].FetchErrors, errs[j])
				continue
			}
			d.FailingBuilds[i].Logs = append(d.FailingBuilds[i].Logs, logs[j])
		}
	}
	return d, nil
}

// selectFailing picks the builds worth extracting logs from, preferring
// descendants (the specific failures) over the parent (their aggregate), and
// skipping failures that produced no log at all.
func selectFailing(page *BuildPage, max int) []Build {
	var withLogs, withoutLogs []Build
	for _, b := range page.Descendants {
		if !b.Failed() {
			continue
		}
		if len(b.LogSteps) > 0 {
			withLogs = append(withLogs, b)
		} else {
			withoutLogs = append(withoutLogs, b)
		}
	}
	if len(withLogs) == 0 && page.Build.Failed() && len(page.Build.LogSteps) > 0 {
		withLogs = append(withLogs, page.Build)
	}
	// Descendants that failed without logs are still worth naming when nothing
	// else is available, so the caller learns which step died.
	if len(withLogs) == 0 {
		withLogs = withoutLogs
	}
	if len(withLogs) > max {
		withLogs = withLogs[:max]
	}
	return withLogs
}

func stateOrUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
