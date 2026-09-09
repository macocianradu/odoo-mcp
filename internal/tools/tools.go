// Package tools exposes the runbot client as MCP tools.
//
// Tools return plain text rather than structured JSON. The runbot data these
// tools carry is mostly log excerpts, and emitting it twice — once as text and
// again as a structured payload — would double the context a model spends on a
// single answer for no gain.
package tools

import (
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/odoo/odoo-mcp/internal/runbot"
)

// Register adds every runbot tool to the server. trigram is the default name
// fragment used by runbot_my_prs when the caller does not supply one; it may be
// empty, in which case that tool requires an explicit argument.
func Register(s *mcp.Server, c *runbot.Client, trigram string) {
	registerMyPRs(s, c, trigram)
	registerPRStatus(s, c)
	registerBuild(s, c)
	registerBuildErrors(s, c)
	registerBuildLog(s, c)
}

// textResult wraps rendered text as a tool result.
func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// errResult reports a failure to the model without failing the call, so the
// model can read the reason and adjust rather than seeing a bare protocol error.
func errResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

// buildLine renders a one-line summary of a build.
func buildLine(b *runbot.Build) string {
	if b == nil {
		return "(no build yet)"
	}
	desc := b.Description
	if desc != "" {
		desc = " — " + desc
	}
	return fmt.Sprintf("build %d [%s]%s", b.ID, resultLabel(b), desc)
}

// resultLabel describes a build in one word, distinguishing "not failed" from
// "not finished", which a bare result field conflates.
func resultLabel(b *runbot.Build) string {
	if b == nil {
		return "no build"
	}
	if b.Done() {
		if b.GlobalResult == "" {
			return "done"
		}
		return b.GlobalResult
	}
	state := b.GlobalState
	if state == "" {
		state = "unknown"
	}
	// A build can already carry a failing result while later steps still run;
	// reporting only the state would hide a failure the caller asked about.
	if b.Failed() {
		return b.GlobalResult + " (" + state + ")"
	}
	return state
}

// renderSnippets writes log excerpts with line numbers.
func renderSnippets(sb *strings.Builder, ex runbot.Extraction) {
	fmt.Fprintf(sb, "  %s (%d lines) %s\n", ex.Step, ex.TotalLines, ex.URL)
	if ex.Note != "" {
		fmt.Fprintf(sb, "    note: %s\n", ex.Note)
	}
	for _, sn := range ex.Snippets {
		fmt.Fprintf(sb, "    --- from line %d (%s) ---\n", sn.StartLine, sn.Reason)
		for i, l := range sn.Lines {
			fmt.Fprintf(sb, "    %6d | %s\n", sn.StartLine+i, l)
		}
	}
}
