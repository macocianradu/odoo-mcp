package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/odoo/odoo-mcp/internal/runbot"
)

type myPRsArgs struct {
	Who         string `json:"who,omitempty" jsonschema:"name fragment to search bundles for, typically an Odoo trigram such as admac; defaults to the RUNBOT_TRIGRAM setting"`
	OnlyFailing bool   `json:"only_failing,omitempty" jsonschema:"report only bundles whose latest batch has a failing trigger"`
	Limit       int    `json:"limit,omitempty" jsonschema:"maximum bundles to report, newest first (default 20)"`
}

const defaultPRLimit = 20

func registerMyPRs(s *mcp.Server, c *runbot.Client, trigram string) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "runbot_my_prs",
		Description: "List the pull requests a person has open on runbot, with the CI state of each. " +
			"Searches runbot bundles by name fragment — Odoo branch names conventionally end in the author's " +
			"trigram, so passing a trigram finds that person's work. Reports every bundle's pull requests across " +
			"repositories and its latest batch result, newest first. Use only_failing to see just what is broken.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args myPRsArgs) (*mcp.CallToolResult, any, error) {
		who := strings.TrimSpace(args.Who)
		if who == "" {
			who = trigram
		}
		if who == "" {
			return errResult(fmt.Errorf("give a name fragment as 'who' (e.g. your trigram), or set RUNBOT_TRIGRAM so it can be omitted")), nil, nil
		}

		// Ask for well beyond what will be reported: the total is part of the
		// answer, and runbot truncates silently if no limit is given.
		bundles, complete, err := c.SearchBundles(ctx, who, searchRowsFor(args.Limit))
		if err != nil {
			return errResult(err), nil, nil
		}

		// The search listing is condensed and can hide a failure, so read the
		// real batch for the bundles about to be reported. Only the window that
		// will actually be shown is resolved, to bound the work.
		withPRs, branchOnly := splitByPRs(bundles)
		runbot.SortBundles(withPRs)

		window := withPRs
		if n := limitOf(args.Limit); len(window) > n {
			window = window[:n]
		}
		c.ResolveStates(ctx, window)

		return textResult(renderMyPRs(window, len(withPRs), branchOnly, complete, who, args)), nil, nil
	})
}

// splitByPRs separates bundles that carry pull requests from plain dev
// branches, which are counted but not listed by a tool about pull requests.
func splitByPRs(bundles []runbot.BundleSummary) (withPRs []runbot.BundleSummary, branchOnly int) {
	for _, b := range bundles {
		if len(b.Bundle.PRs) == 0 {
			branchOnly++
			continue
		}
		withPRs = append(withPRs, b)
	}
	return withPRs, branchOnly
}

func limitOf(n int) int {
	if n <= 0 {
		return defaultPRLimit
	}
	return n
}

// searchRowsFor sizes the search so the reported total is trustworthy, and so
// that raising the report limit also widens the search behind it.
func searchRowsFor(reportLimit int) int {
	if n := limitOf(reportLimit) * 4; n > runbot.DefaultSearchRows {
		return n
	}
	return runbot.DefaultSearchRows
}

func renderMyPRs(window []runbot.BundleSummary, totalWithPRs, branchOnly int, complete bool, who string, args myPRsArgs) string {
	var sb strings.Builder

	failing := 0
	for _, b := range window {
		if b.Overall == runbot.StateFailure {
			failing++
		}
	}

	shown := window
	if args.OnlyFailing {
		var only []runbot.BundleSummary
		for _, b := range window {
			if b.Overall == runbot.StateFailure {
				only = append(only, b)
			}
		}
		shown = only
	}

	total := fmt.Sprintf("%d", totalWithPRs)
	if !complete {
		// Runbot capped the result set, so the total is a floor, not a count.
		total += "+"
	}
	fmt.Fprintf(&sb, "pull requests matching %q — %s found, %d most recent checked, %d failing",
		who, total, len(window), failing)
	if branchOnly > 0 {
		fmt.Fprintf(&sb, " (%d further bundles have no PR)", branchOnly)
	}
	sb.WriteString("\n")
	if !complete {
		sb.WriteString("Runbot capped the search: more matches exist beyond those counted. Raise limit to widen it.\n")
	}

	if len(shown) == 0 {
		if args.OnlyFailing && len(window) > 0 {
			sb.WriteString("\nNothing is failing among the PRs checked.\n")
		} else {
			sb.WriteString("\nNo pull requests found. Note that this matches bundle names, not a GitHub author field.\n")
		}
		return sb.String()
	}

	for _, b := range shown {
		fmt.Fprintf(&sb, "\n%-8s %s\n", b.Overall, b.Bundle.Name)
		fmt.Fprintf(&sb, "         %s\n", prList(b.Bundle.PRs))
		if b.BatchID == 0 {
			sb.WriteString("         no batch has run yet\n")
			continue
		}
		fmt.Fprintf(&sb, "         batch %d · %s\n", b.BatchID, b.Bundle.URL)
		for _, slot := range b.Failing() {
			fmt.Fprintf(&sb, "         FAILING %s — %s\n", slot.Trigger, buildLine(slot.Build))
		}
		if b.Partial {
			sb.WriteString("         (batch could not be read; state may be incomplete)\n")
		}
	}

	if rest := totalWithPRs - len(window); rest > 0 {
		fmt.Fprintf(&sb, "\n[%d older PRs not checked; raise limit to include them]\n", rest)
	}
	return sb.String()
}

func prList(prs []runbot.PullRequest) string {
	out := make([]string, 0, len(prs))
	for _, p := range prs {
		out = append(out, fmt.Sprintf("%s#%d", p.Repo, p.Number))
	}
	return strings.Join(out, ", ")
}
