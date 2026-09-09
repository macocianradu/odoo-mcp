// Command odoo-mcp serves Odoo runbot CI data over the Model Context Protocol.
//
// It reads only public runbot pages and log files, so it needs no credentials
// for odoo/odoo, odoo/enterprise, design-themes or upgrade. GitHub data is
// deliberately out of scope: existing GitHub MCP servers already cover it.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/odoo/odoo-mcp/internal/runbot"
	"github.com/odoo/odoo-mcp/internal/tools"
)

const version = "0.1.0"

func main() {
	var (
		baseURL = flag.String("runbot-url", envOr("RUNBOT_URL", runbot.DefaultBaseURL), "runbot base URL")
		project = flag.String("runbot-project", envOr("RUNBOT_PROJECT", runbot.DefaultProject), "runbot project slug used for PR search")
		trigram = flag.String("trigram", envOr("RUNBOT_TRIGRAM", ""), "default name fragment for runbot_my_prs, typically your Odoo trigram")
		check   = flag.Bool("check", false, "verify connectivity to runbot and exit")
		showVer = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("odoo-mcp", version)
		return
	}

	client := runbot.New(*baseURL, *project)

	if *check {
		if err := selfCheck(client); err != nil {
			// stderr: stdout carries the MCP protocol and must stay clean.
			fmt.Fprintf(os.Stderr, "odoo-mcp: check failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("odoo-mcp %s: %s (project %s) reachable\n", version, client.BaseURL, client.Project)
		return
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "odoo-runbot", Version: version}, nil)
	tools.Register(server, client, *trigram)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "odoo-mcp: %v\n", err)
		os.Exit(1)
	}
}

// selfCheck confirms runbot is reachable and still parses as expected, which
// distinguishes a network problem from a frontend change.
func selfCheck(c *runbot.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	page, err := c.Build(ctx, 124585551)
	if err != nil {
		return err
	}
	if page.Build.Host == "" {
		return fmt.Errorf("build page parsed but carried no host; the runbot frontend may have changed")
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
