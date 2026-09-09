package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/odoo/odoo-mcp/internal/runbot"
)

type buildLogArgs struct {
	BuildID int    `json:"build_id" jsonschema:"runbot build id"`
	Step    string `json:"step,omitempty" jsonschema:"log step to read, e.g. install_all or start_test_lint; omit to list the steps this build produced"`
	Grep    string `json:"grep,omitempty" jsonschema:"optional case-insensitive regular expression; only matching lines are returned"`
	Tail    int    `json:"tail,omitempty" jsonschema:"return only the last N matching lines (default 200)"`
}

const (
	defaultTail = 200
	// maxLogBytes caps a single log response. Runbot logs reach megabytes, and
	// returning one whole would displace everything else in the context.
	maxLogBytes = 50000
)

func registerBuildLog(s *mcp.Server, c *runbot.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "runbot_build_log",
		Description: "Read a runbot build's raw log, optionally filtered by a regular expression. " +
			"Call without a step to list the logs a build produced. Output is capped, so narrow the search with " +
			"grep rather than expecting a whole log. Prefer runbot_build_errors first; use this when its digest is not enough.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args buildLogArgs) (*mcp.CallToolResult, any, error) {
		if args.BuildID <= 0 {
			return errResult(fmt.Errorf("build_id must be a positive build id, got %d", args.BuildID)), nil, nil
		}

		var re *regexp.Regexp
		if args.Grep != "" {
			var err error
			if re, err = regexp.Compile("(?i)" + args.Grep); err != nil {
				return errResult(fmt.Errorf("invalid grep pattern %q: %w", args.Grep, err)), nil, nil
			}
		}

		page, err := c.Build(ctx, args.BuildID)
		if err != nil {
			return errResult(err), nil, nil
		}
		b := page.Build

		if args.Step == "" {
			return textResult(renderLogIndex(ctx, c, b)), nil, nil
		}
		if b.Host == "" || b.Dest == "" {
			return errResult(fmt.Errorf("build %d has no host or destination, so it has no logs to read", b.ID)), nil, nil
		}

		step := strings.TrimSuffix(args.Step, ".txt")
		url := b.LogURL(step)
		lines, err := c.FetchLog(ctx, url)
		if err != nil {
			if runbot.NotFound(err) {
				return errResult(fmt.Errorf("build %d has no log step %q; available: %s",
					b.ID, step, strings.Join(b.LogSteps, ", "))), nil, nil
			}
			return errResult(err), nil, nil
		}

		return textResult(renderLog(b, step, url, lines, re, args.Tail)), nil, nil
	})
}

func renderLogIndex(ctx context.Context, c *runbot.Client, b runbot.Build) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n%s\n\n", buildLine(&b), b.URL)

	files, err := c.ListLogs(ctx, b.Host, b.Dest)
	if err != nil || len(files) == 0 {
		if len(b.LogSteps) == 0 {
			sb.WriteString("This build produced no logs.\n")
			return sb.String()
		}
		fmt.Fprintf(&sb, "log steps: %s\n", strings.Join(b.LogSteps, ", "))
		return sb.String()
	}

	sb.WriteString("available logs:\n")
	for _, f := range files {
		fmt.Fprintf(&sb, "  %-44s %9d bytes\n", f.Name, f.Size)
	}
	fmt.Fprintf(&sb, "\nCall again with step=<name without .txt> to read one.\n")
	return sb.String()
}

func renderLog(b runbot.Build, step, url string, lines []string, re *regexp.Regexp, tail int) string {
	total := len(lines)

	type numbered struct {
		n    int
		text string
	}
	var kept []numbered
	for i, l := range lines {
		if re != nil && !re.MatchString(l) {
			continue
		}
		kept = append(kept, numbered{n: i + 1, text: l})
	}
	matched := len(kept)

	if tail <= 0 {
		tail = defaultTail
	}
	omitted := 0
	if len(kept) > tail {
		omitted = len(kept) - tail
		kept = kept[len(kept)-tail:]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "build %d step %s — %d lines total", b.ID, step, total)
	if re != nil {
		fmt.Fprintf(&sb, ", %d matching", matched)
	}
	fmt.Fprintf(&sb, "\n%s\n\n", url)
	if omitted > 0 {
		fmt.Fprintf(&sb, "[... %d earlier lines omitted; showing the last %d ...]\n", omitted, len(kept))
	}

	truncated := 0
	for i, k := range kept {
		if sb.Len() > maxLogBytes {
			truncated = len(kept) - i
			break
		}
		fmt.Fprintf(&sb, "%6d | %s\n", k.n, k.text)
	}
	if truncated > 0 {
		fmt.Fprintf(&sb, "[... %d more lines truncated at the output limit; narrow with grep or a smaller tail ...]\n", truncated)
	}
	if matched == 0 && re != nil {
		sb.WriteString("(no lines matched)\n")
	}
	return sb.String()
}
