# Token X

A small native desktop app that shows token usage and cost for the AI coding
agents on your machine, and lets you permanently delete sessions you no
longer need.

It reads each agent's local data directly — there is no index database, no
account, and no network access.

## Supported agents

| Agent | Data it reads | Cost |
|---|---|---|
| Codex CLI | rollout JSONL files (including `.zst` archives) | estimated from `prices.json` |
| OpenCode | local SQLite store (read-only) | recorded, if present |
| Claude Code | session transcripts under `~/.claude/projects` | `costUSD` in the transcript, else estimated |
| Gemini CLI | session files under `~/.gemini/tmp` | estimated from `prices.json` |

An agent whose data directory is missing is simply skipped.

## Features

- A hero number for the selected period (today / last 7 days / last 30 days),
  with a GitHub-style daily heat grid below it.
- A sessions table with search, project/source filters, and sorting by
  tokens, cost or time. Model names have tooltips when truncated.
- Permanent deletion of one or many sessions, with a confirmation dialog.
  This cannot be undone.
- Light/dark theme with an orange accent.
- Prices are read from `~/.token-x/prices.json`; there is no settings UI.
- Chinese UI (the language of the installer and the app's own strings).

## Build

Requires Go and the `mygo` tool (pinned in `go.mod`):

```sh
go build ./...
go test ./...

# Development build
go tool mygo dev

# Production build, including the Windows NSIS installer
go tool mygo build
```

The packaged app lands in `build/windows-amd64/`.

## Data and privacy

All reads happen on your machine. The app never uploads session contents.
Deleting a session removes its data from disk permanently — there is no
recycle bin and no undo.
