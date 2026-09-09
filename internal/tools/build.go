package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/odoo/odoo-mcp/internal/runbot"
)

type buildArgs struct {
	BuildID int `json:"build_id" jsonschema:"runbot build id, as shown by runbot_pr_status or in a runbot URL"`
}

func registerBuild(s *mcp.Server, c *runbot.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "runbot_build",
		Description: "Show one runbot build: its state and result, the host it ran on, the log steps it produced, " +
			"and its child builds with their individual results. Useful for navigating from a parent build to the " +
			"child that actually failed. To read errors directly, prefer runbot_build_errors.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args buildArgs) (*mcp.CallToolResult, any, error) {
		if args.BuildID <= 0 {
			return errResult(fmt.Errorf("build_id must be a positive build id, got %d", args.BuildID)), nil, nil
		}
		page, err := c.Build(ctx, args.BuildID)
		if err != nil {
			return errResult(err), nil, nil
		}
		return textResult(renderBuild(page)), nil, nil
	})
}

func renderBuild(page *runbot.BuildPage) string {
	var sb strings.Builder
	b := page.Build

	fmt.Fprintf(&sb, "%s\n%s\n", buildLine(&b), b.URL)
	fmt.Fprintf(&sb, "state=%s result=%s host=%s dest=%s\n",
		orDash(b.GlobalState), orDash(b.GlobalResult), orDash(b.Host), orDash(b.Dest))

	if len(b.LogSteps) > 0 {
		fmt.Fprintf(&sb, "log steps: %s\n", strings.Join(b.LogSteps, ", "))
	} else {
		sb.WriteString("log steps: none (this build wrote no logs)\n")
	}

	if len(page.Descendants) == 0 {
		return sb.String()
	}

	failing := 0
	for _, d := range page.Descendants {
		if d.Failed() {
			failing++
		}
	}
	fmt.Fprintf(&sb, "\n%d child build(s), %d failing:\n", len(page.Descendants), failing)
	for _, d := range page.Descendants {
		mark := " "
		if d.Failed() {
			mark = "*"
		}
		fmt.Fprintf(&sb, " %s %-12s %s\n", mark, resultLabel(&d), buildLine(&d))
	}
	return sb.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
