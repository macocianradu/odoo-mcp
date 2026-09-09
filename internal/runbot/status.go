package runbot

import (
	"context"
	"fmt"
)

// Overall CI states reported for a pull request.
const (
	StateSuccess = "success"
	StateFailure = "failure"
	StatePending = "pending"
	StateUnknown = "unknown"
)

// PRStatus is runbot's view of a pull request: the bundle testing it, that
// bundle's most recent batch, and the per-trigger results.
type PRStatus struct {
	PR      PullRequest `json:"pull_request"`
	Bundle  Bundle      `json:"bundle"`
	Batch   *Batch      `json:"latest_batch,omitempty"`
	Overall string      `json:"overall"`
	Failing []Slot      `json:"failing_triggers,omitempty"`
}

// PRStatus resolves a pull request to its current runbot results.
func (c *Client) PRStatus(ctx context.Context, repo string, number int) (*PRStatus, error) {
	bundle, err := c.FindBundleForPR(ctx, repo, number)
	if err != nil {
		return nil, err
	}

	st := &PRStatus{Bundle: *bundle, Overall: StateUnknown}
	repo = NormalizeRepo(repo)
	for _, pr := range bundle.PRs {
		if pr.Number == number && pr.Repo == repo {
			st.PR = pr
			break
		}
	}

	if len(bundle.Batches) == 0 {
		return st, nil
	}
	batch, err := c.Batch(ctx, bundle.Batches[0])
	if err != nil {
		// The bundle resolved; report it rather than losing that work.
		return st, fmt.Errorf("runbot: resolved bundle %d but could not read its latest batch: %w", bundle.ID, err)
	}
	st.Batch = batch
	st.Overall, st.Failing = summarize(batch.Slots)
	return st, nil
}

// summarize reduces per-trigger results to one state. A failure anywhere makes
// the whole thing a failure; otherwise anything unfinished makes it pending.
func summarize(slots []Slot) (string, []Slot) {
	var failing []Slot
	pending := false
	seen := false

	for _, s := range slots {
		if s.Build == nil {
			pending = true
			continue
		}
		seen = true
		switch {
		case s.Build.Failed():
			failing = append(failing, s)
		case !s.Build.Done():
			pending = true
		}
	}

	switch {
	case len(failing) > 0:
		return StateFailure, failing
	case pending:
		return StatePending, nil
	case seen:
		return StateSuccess, nil
	default:
		return StateUnknown, nil
	}
}
