# odoo-mcp

An MCP server that lets an AI assistant check Odoo runbot CI status and read build
failures for pull requests on `odoo/odoo`, `odoo/enterprise`, `design-themes` and
`upgrade`.

Basically, it answers "why is my PR red?" without you leaving the editor. It takes a
pull request, finds its runbot bundle, works out which trigger failed, digs down to
the child build that actually broke, and pulls the useful part of that build's log:
the traceback, the assertion message, the lint findings.

## Nothing to configure

There is no setup step and no credentials to supply. The server is read-only: it
never triggers a rebuild, a kill or a wake up.

GitHub data is deliberately out of scope, since existing GitHub MCP servers already
cover pull request metadata. This server sticks to the runbot side.

## Install

```sh
go build -o odoo-mcp .
./odoo-mcp -check      # verify runbot is reachable and still parses
```

Register it with Claude Code:

```sh
claude mcp add odoo-runbot -- /absolute/path/to/odoo-mcp
```

or add it to `.mcp.json` / `~/.claude.json`:

```json
{
  "mcpServers": {
    "odoo-runbot": {
      "command": "/absolute/path/to/odoo-mcp"
    }
  }
}
```

## Tools

| Tool | Purpose |
|---|---|
| `runbot_my_prs` | Every PR you have open, with the CI state of each. Searches bundles by name fragment |
| `runbot_pr_status` | Overall CI state of a PR, every trigger's result with its build id, and sibling PRs tested in the same bundle |
| `runbot_build_errors` | Why a build or PR failed. Walks to the failing child and extracts the error from its log |
| `runbot_build` | One build's state, host, log steps and child builds |
| `runbot_build_log` | Raw log access with a regex filter, for when the digest is not enough |

`runbot_build_errors` takes either a `build_id` or `repo` plus `pr`, so the common
question is a single call.

### Finding your own PRs

Odoo branch names conventionally end in the author's trigram, so `runbot_my_prs`
searches bundle names for one:

```sh
./odoo-mcp -trigram admac     # or RUNBOT_TRIGRAM=admac
```

With that set, the tool needs no arguments. Keep in mind it is a substring match on
bundle names rather than a GitHub author field, so an unrelated bundle can match.

Runbot's search route returns only **40 rows** unless you pass a `limit`, and it
gives no sign that it truncated anything. A naive caller quietly loses older matches
and reports a total that is really just the cap. The client here always sends a
limit, and marks the total with `+` if runbot capped the result set anyway.

There is a second catch: the search listing runbot returns is *condensed*. It renders
only a couple of triggers per batch, so a bundle whose visible trigger is still
running can look "pending" while another trigger has already failed. To avoid that,
the tool reads the real batch page for every PR it reports, and flags any it could
not read instead of presenting a guess as fact. That is also why it checks a bounded
window (`limit`, default 20) rather than every match.

### Bundles span repositories

This is the most useful thing runbot knows that GitHub does not. A feature branch
pushed to several repos under the same name is *one bundle* with *one* set of builds.
So `runbot_pr_status` on an enterprise PR will also report the community and
design-themes PRs it is tested with, and a failure may well originate in a sibling.

## Configuration

Only needed for a non-default runbot:

| Flag | Env | Default |
|---|---|---|
| `-runbot-url` | `RUNBOT_URL` | `https://runbot.odoo.com` |
| `-runbot-project` | `RUNBOT_PROJECT` | `rd-1` |
| `-trigram` | `RUNBOT_TRIGRAM` | (none) |

The project slug scopes runbot's PR search route.

## Development

```sh
go test ./...                  # unit tests, offline, against saved fixtures
go test -tags=live ./...       # smoke tests against the real runbot
```

Unit tests run against HTML fixtures in `internal/runbot/testdata`, so they stay fast
and offline. The live tests exist because this server reads runbot's HTML: if the
frontend changes, they fail loudly and name the selector that broke instead of
letting the tools silently return nothing.

### How the data is read

Runbot renders each build as a `<build-options-dropdown>` custom element whose
`data-*` attributes carry raw ORM values (`data-global_result`, `data-host`,
`data-dest`, `data-log_list`). Reading those is a lot more stable than parsing the
presentational markup around them.

Log files come from `http://<host>/runbot/static/build/<dest>/logs/<step>.txt`.
Note the plain `http`: those hosts do not serve TLS, so requests must not be
upgraded.

### Failure extraction

Odoo logs are handled record-wise. A log record starts with a timestamp, so a failing
`ERROR` record carries every following non-timestamped line with it. That keeps a
traceback together with the assertion message printed after it, which is usually
where the real cause hides.

Logs in other formats fall back to marker matching, and a log with no recognisable
markers falls back to its tail. That last fallback matters more than it sounds: a
build can fail without its log containing the word "error" at all. Semgrep, for
instance, reports findings to a JSON file and only summarises them in the log.

All output is capped, and every excerpt comes back with the log URL so the full file
stays one click away.

## Limitations

- Reading runbot's HTML is inherently sensitive to frontend changes. The live tests
  are the early-warning system.
- Extraction heuristics do not cover every step type. Unrecognised formats degrade to
  the tail of the log rather than to nothing.
- A pull request too new for runbot to have picked up, or one that is closed, will
  not resolve to a bundle.
- CI state is genuinely transient. Runbot retries builds, so a trigger reported as
  failed can be back to `waiting` moments later, and a result can flip either way
  between two calls. A `ko` that clears on retry usually means a flaky test.

## License

Copyright (C) 2026 Odoo S.A.

This program is free software: you can redistribute it and/or modify it under the
terms of the GNU Lesser General Public License as published by the Free Software
Foundation, either version 3 of the License, or (at your option) any later version.
See [LICENSE](LICENSE) for the full text, which includes the GPLv3 that the LGPL
builds on.

It is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY,
without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR
PURPOSE.
