# Token X

A small native desktop app that shows token usage and cost for the AI coding
agents on your machine, and lets you permanently delete sessions you no
longer need.

It reads each agent's local data directly — no account, and no session data
ever leaves your machine. Once at launch it fetches LiteLLM's public model
price list (with a CDN fallback) and caches it locally; a failed fetch simply
falls back to cached or built-in prices.

## Supported agents

| Agent | Data it reads | Cost |
|---|---|---|
| Codex CLI | rollout JSONL files (including `.zst` archives) | estimated from the price table |
| OpenCode | local SQLite store (read-only) | recorded, if present |
| Claude Code | session transcripts under `~/.claude/projects` | `costUSD` in the transcript, else estimated |
| Gemini CLI | session files under `~/.gemini/tmp` | estimated from the price table |
| WorkBuddy | session transcripts under `~/.workbuddy/projects` | estimated from the price table |

An agent whose data directory is missing is simply skipped.

## Features

- A hero number for the selected period (today / last 7 days / last 30 days),
  with a GitHub-style daily heat grid below it.
- A sessions table with search, project/source filters, and sorting by
  tokens, cost or time. Model names have tooltips when truncated.
- Permanent deletion of one or many sessions, with a confirmation dialog.
  This cannot be undone.
- Light/dark theme with an orange accent: follows the system until you
  switch, then remembers your choice across runs. The custom title bar uses
  Windows buttons on Windows and traffic lights on macOS.
- Prices come from three layers, in order: your `~/.token-x/prices.json`,
  the online list fetched at launch, and built-in defaults. There is no
  settings UI — edit that file to override any model. Unknown models (such
  as WorkBuddy's `hy3` or `glm-5.3`) use the fallback price.
- Historical amounts are stable: once a session has been read, its cost is
  cached and never recomputed with a newer price list. If the agent itself
  recorded a cost (OpenCode / Claude Code), that value always wins.
- The session snapshot is cached in `~/.token-x/sessions.json`. Later launches
  show it immediately and only re-read transcripts that actually changed;
  the first scan of a fresh install is still the slow case.
- Chinese UI (the language of the installer and the app's own strings).

## Build

Requires Go and the `mygo` tool (pinned in `go.mod`):

```sh
go build ./...
go test ./...

# Development build
go tool mygo dev

# Production build (Windows NSIS installer)
go tool mygo build

# macOS apps (.app and .dmg for both M series / arm64 and Intel / amd64;
# run on a Mac to produce the dmg)
go tool mygo build -platform darwin/arm64,darwin/amd64 -sign -
```

The packaged app lands in `build/windows-amd64/`, `build/darwin-arm64/` or
`build/darwin-amd64/`.
On macOS, open the `.dmg` and drag Token X to Applications; the `.app` is
ad-hoc signed unless a Developer ID is supplied with `-sign`.

## Data and privacy

Session data is read and deleted only on your machine — nothing about your
sessions is uploaded. The single outbound request is a GET for a static
price list from GitHub (or jsDelivr as a fallback); no session contents,
paths or identifiers are sent with it.
Deleting a session removes its data from disk permanently — there is no
recycle bin and no undo.
