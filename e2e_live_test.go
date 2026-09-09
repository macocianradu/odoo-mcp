//go:build live

// End-to-end tests that launch the built server and drive it as an MCP client,
// exercising tool schemas and rendering the way a real client would.
// Run with: go test -tags=live -run TestE2E -v .
package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func connect(t *testing.T) (*mcp.ClientSession, context.Context) {
	t.Helper()
	bin := t.TempDir() + "/odoo-mcp"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("building server: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "test"}, nil)
	sess, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(bin)}, nil)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(func() { sess.Close() })
	return sess, ctx
}

func callText(t *testing.T, sess *mcp.ClientSession, ctx context.Context, name string, args map[string]any) string {
	t.Helper()
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	if res.IsError {
		t.Logf("tool reported an error: %s", sb.String())
	}
	return sb.String()
}

// skipIfFixtureGone lets an end-to-end test skip when runbot has garbage
// collected a pinned build, rather than reporting it as a regression.
func skipIfFixtureGone(t *testing.T, out string) {
	t.Helper()
	if strings.Contains(out, "HTTP 404") {
		t.Skipf("fixture no longer on runbot: %s", out)
	}
}

func TestE2EListTools(t *testing.T) {
	sess, ctx := connect(t)
	res, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := map[string]bool{
		"runbot_pr_status": false, "runbot_build": false,
		"runbot_build_errors": false, "runbot_build_log": false,
	}
	for _, tool := range res.Tools {
		t.Logf("tool %s", tool.Name)
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("%s has no input schema", tool.Name)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %s not registered", name)
		}
	}
}

func TestE2EPRStatusEnterprise(t *testing.T) {
	sess, ctx := connect(t)
	out := callText(t, sess, ctx, "runbot_pr_status", map[string]any{"repo": "odoo/enterprise", "pr": 130652})
	skipIfFixtureGone(t, out)
	t.Logf("\n%s", out)
	for _, want := range []string{"odoo/enterprise#130652", "bundle", "Community Run", "Enterprise Run"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	// The cross-repo bundle view is the point: sibling PRs must be surfaced.
	if !strings.Contains(out, "odoo/odoo#286248") {
		t.Error("sibling pull request not reported")
	}
}

func TestE2EBuildErrors(t *testing.T) {
	sess, ctx := connect(t)
	out := callText(t, sess, ctx, "runbot_build_errors", map[string]any{"build_id": 124593755})
	skipIfFixtureGone(t, out)
	t.Logf("\n%s", out)
	for _, want := range []string{"FAIL: TestESLint.test_eslint", "AssertionError", "no-unused-vars"} {
		if !strings.Contains(out, want) {
			t.Errorf("diagnosis missing %q", want)
		}
	}
}

func TestE2EBuildLogIndexAndGrep(t *testing.T) {
	sess, ctx := connect(t)

	idx := callText(t, sess, ctx, "runbot_build_log", map[string]any{"build_id": 124593755})
	skipIfFixtureGone(t, idx)
	t.Logf("index:\n%s", idx)
	if !strings.Contains(idx, "start_test_lint") {
		t.Error("log index did not list start_test_lint")
	}

	grep := callText(t, sess, ctx, "runbot_build_log", map[string]any{
		"build_id": 124593755, "step": "start_test_lint", "grep": "FAIL:", "tail": 5,
	})
	t.Logf("grep:\n%s", grep)
	if !strings.Contains(grep, "TestESLint") {
		t.Error("grep did not find the failing test")
	}
	if len(grep) > 60000 {
		t.Errorf("grep output %d bytes, exceeds the cap", len(grep))
	}
}

func TestE2EBadInput(t *testing.T) {
	sess, ctx := connect(t)
	if out := callText(t, sess, ctx, "runbot_build_errors", nil); !strings.Contains(out, "build_id") {
		t.Errorf("expected guidance about required arguments, got %q", out)
	}
	if out := callText(t, sess, ctx, "runbot_pr_status", map[string]any{"repo": "odoo/odoo", "pr": 999999999}); out == "" {
		t.Error("expected an explanatory message for a nonexistent PR")
	}
}

func TestE2EMyPRs(t *testing.T) {
	sess, ctx := connect(t)

	all := callText(t, sess, ctx, "runbot_my_prs", map[string]any{"who": "admac", "limit": 6})
	t.Logf("\n%s", all)
	if !strings.Contains(all, "pull requests matching") {
		t.Error("missing header")
	}
	if !strings.Contains(all, "odoo/") {
		t.Error("no pull requests listed")
	}
	// Runbot's search caps at 40 rows unless a limit is sent. The reported
	// total must reflect the whole result set, not that cap.
	if strings.Contains(all, "40 found") {
		t.Error("total still reflects runbot's un-limited 40-row cap")
	}
	if strings.Contains(all, "capped the search") {
		t.Error("default search should not be hitting the cap for this author")
	}

	failing := callText(t, sess, ctx, "runbot_my_prs", map[string]any{"who": "admac", "only_failing": true})
	t.Logf("\nonly_failing:\n%s", failing)

	// Without a trigram configured the tool must say how to supply one.
	none := callText(t, sess, ctx, "runbot_my_prs", map[string]any{"who": "zzzzznotarealtrigramzzzzz"})
	if !strings.Contains(none, "No pull requests found") {
		t.Errorf("unexpected empty-search output: %s", none)
	}
}
