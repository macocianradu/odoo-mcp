package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/odoo/odoo-mcp/internal/runbot"
)

type buildErrorsArgs struct {
	BuildID int    `json:"build_id,omitempty" jsonschema:"runbot build id to diagnose; give this or repo+pr"`
	Repo    string `json:"repo,omitempty" jsonschema:"repository of a pull request to diagnose, e.g. odoo/odoo or odoo/enterprise"`
	PR      int    `json:"pr,omitempty" jsonschema:"pull request number to diagnose; every failing trigger is inspected"`
}

// maxDiagnosedTriggers bounds how many failing triggers one pull-request
// diagnosis will chase, so a badly broken PR cannot flood the context.
const maxDiagnosedTriggers = 3

func registerBuildErrors(s *mcp.Server, c *runbot.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "runbot_build_errors",
		Description: "Explain why a runbot build or pull request failed. Walks down to the child build that actually " +
			"failed, then extracts the relevant part of its log — test tracebacks, assertion messages, lint findings. " +
			"Accepts either a build_id, or repo plus pr to diagnose every failing trigger of a pull request. " +
			"Output is truncated; use runbot_build_log for more of a specific log.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args buildErrorsArgs) (*mcp.CallToolResult, any, error) {
		switch {
		case args.BuildID > 0:
			d, err := c.Diagnose(ctx, args.BuildID, runbot.DiagnoseOptions{})
			if err != nil {
				return errResult(err), nil, nil
			}
			return textResult(renderDiagnosis(d)), nil, nil

		case args.PR > 0:
			return diagnosePR(ctx, c, args.Repo, args.PR)

		default:
			return errResult(fmt.Errorf("give either build_id, or repo and pr")), nil, nil
		}
	})
}

func diagnosePR(ctx context.Context, c *runbot.Client, repo string, pr int) (*mcp.CallToolResult, any, error) {
	st, err := c.PRStatus(ctx, repo, pr)
	if err != nil {
		return errResult(err), nil, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s#%d — %s (bundle %q)\n", runbot.NormalizeRepo(repo), pr, st.Overall, st.Bundle.Name)

	if len(st.Failing) == 0 {
		switch st.Overall {
		case runbot.StatePending:
			sb.WriteString("\nNothing has failed; some triggers are still running.\n")
		case runbot.StateSuccess:
			sb.WriteString("\nAll triggers passed; there is no failure to explain.\n")
		default:
			sb.WriteString("\nNo failing trigger found.\n")
		}
		return textResult(sb.String()), nil, nil
	}

	failing := st.Failing
	if len(failing) > maxDiagnosedTriggers {
		fmt.Fprintf(&sb, "\n%d triggers failed; showing the first %d.\n", len(failing), maxDiagnosedTriggers)
		failing = failing[:maxDiagnosedTriggers]
	}

	for _, slot := range failing {
		fmt.Fprintf(&sb, "\n=== %s ===\n", slot.Trigger)
		if slot.Build == nil {
			sb.WriteString("no build for this trigger\n")
			continue
		}
		d, err := c.Diagnose(ctx, slot.Build.ID, runbot.DiagnoseOptions{})
		if err != nil {
			fmt.Fprintf(&sb, "could not diagnose build %d: %v\n", slot.Build.ID, err)
			continue
		}
		sb.WriteString(renderDiagnosis(d))
	}
	return textResult(sb.String()), nil, nil
}

func renderDiagnosis(d *runbot.Diagnosis) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n%s\n", buildLine(&d.Build), d.Build.URL)

	if d.Note != "" {
		fmt.Fprintf(&sb, "%s\n", d.Note)
	}
	for _, fb := range d.FailingBuilds {
		if fb.Build.ID != d.Build.ID {
			fmt.Fprintf(&sb, "\nfailing child: %s\n%s\n", buildLine(&fb.Build), fb.Build.URL)
		}
		if len(fb.Logs) == 0 && len(fb.FetchErrors) == 0 {
			sb.WriteString("  (no logs)\n")
		}
		for _, ex := range fb.Logs {
			renderSnippets(&sb, ex)
		}
		for _, e := range fb.FetchErrors {
			fmt.Fprintf(&sb, "  could not read log: %s\n", e)
		}
	}
	return sb.String()
}
