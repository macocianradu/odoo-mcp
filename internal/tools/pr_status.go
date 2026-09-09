package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/odoo/odoo-mcp/internal/runbot"
)

type prStatusArgs struct {
	Repo string `json:"repo" jsonschema:"repository holding the pull request, e.g. odoo/odoo or odoo/enterprise (a bare name like 'enterprise' is accepted)"`
	PR   int    `json:"pr" jsonschema:"pull request number"`
}

func registerPRStatus(s *mcp.Server, c *runbot.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "runbot_pr_status",
		Description: "Report runbot CI status for a pull request on odoo/odoo, odoo/enterprise, design-themes or upgrade. " +
			"Returns the overall state, the result of every CI trigger (Community Run, Enterprise Run, Check Style, ...) " +
			"with its build id, and any sibling pull requests tested in the same bundle. " +
			"Use runbot_build_errors to find out why a failing trigger failed.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args prStatusArgs) (*mcp.CallToolResult, any, error) {
		if args.PR <= 0 {
			return errResult(fmt.Errorf("pr must be a positive pull request number, got %d", args.PR)), nil, nil
		}
		st, err := c.PRStatus(ctx, args.Repo, args.PR)
		if err != nil {
			return errResult(err), nil, nil
		}
		return textResult(renderPRStatus(st, args.Repo, args.PR)), nil, nil
	})
}

func renderPRStatus(st *runbot.PRStatus, repo string, pr int) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "%s#%d — %s\n", runbot.NormalizeRepo(repo), pr, st.Overall)
	fmt.Fprintf(&sb, "bundle %q: %s\n", st.Bundle.Name, st.Bundle.URL)

	// A bundle spans repositories, so a failure may originate in a sibling PR.
	if siblings := siblingsOf(st.Bundle.PRs, repo, pr); len(siblings) > 0 {
		fmt.Fprintf(&sb, "tested together with: %s\n", strings.Join(siblings, ", "))
	}

	if st.Batch == nil {
		sb.WriteString("\nNo batch has run for this bundle yet.\n")
		return sb.String()
	}

	fmt.Fprintf(&sb, "\nlatest batch %d (%s)\n", st.Batch.ID, st.Batch.URL)
	for _, slot := range st.Batch.Slots {
		fmt.Fprintf(&sb, "  %-12s %-26s %s\n", resultLabel(slot.Build), slot.Trigger, buildID(slot.Build))
	}

	if len(st.Failing) > 0 {
		sb.WriteString("\nfailing:\n")
		for _, slot := range st.Failing {
			fmt.Fprintf(&sb, "  %s — %s\n", slot.Trigger, buildLine(slot.Build))
		}
		fmt.Fprintf(&sb, "\nRun runbot_build_errors with repo=%s pr=%d (or a build id above) to see the failure detail.\n",
			runbot.NormalizeRepo(repo), pr)
	}
	return sb.String()
}

func siblingsOf(prs []runbot.PullRequest, repo string, pr int) []string {
	repo = runbot.NormalizeRepo(repo)
	var out []string
	for _, p := range prs {
		if p.Number == pr && p.Repo == repo {
			continue
		}
		out = append(out, fmt.Sprintf("%s#%d", p.Repo, p.Number))
	}
	return out
}

func buildID(b *runbot.Build) string {
	if b == nil {
		return ""
	}
	return fmt.Sprintf("build %d", b.ID)
}
