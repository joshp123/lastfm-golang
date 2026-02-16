---
name: lastfm
description: Query your local Last.fm scrobble history (via lastfm-golang) for playlist seeds and taste-over-time summaries.
metadata: {"openclaw":{"requires":{"bins":["lastfm-golang"],"env":["LASTFM_ENV_FILE"]},"primaryEnv":"LASTFM_ENV_FILE"}}
---

# lastfm

Use this skill to:

- generate playlist seeds from the user's listening history
- summarize taste over time (yearly top artists + signature artists)
- find resurfacing picks (old favorites not played recently)

This skill assumes the `lastfm-golang` CLI is installed and a local dump exists.

## Preferred command: digest (LLM-friendly JSON)

Run:

```bash
lastfm-golang digest
```

## Discovery: recommend (LLM-friendly JSON)

Run:

```bash
lastfm-golang recommend
```

This returns candidate tracks (from Last.fm similar artists + top tracks), annotated with your local play counts.

Unix-friendly (no JSON parsing): output TSV `artist<TAB>track`:

```bash
lastfm-golang recommend --format tsv
```

- stdout: JSON (safe to parse and feed back into the model)
- stderr: logs / diagnostics

If the command is not found inside a sandbox, re-run with host execution.

## Refreshing data

Incremental sync (uses `$LASTFM_ENV_FILE`):

```bash
lastfm-golang sync
```

One-time full backfill (slow):

```bash
lastfm-golang backfill
```

## Data location

By default:

- `${XDG_DATA_HOME:-~/.local/share}/lastfm-golang/`
  - `lastfm.sqlite`
  - `scrobbles.raw.jsonl`

## Notes

- Some scrobbles may have placeholder 1970 timestamps from Last.fm. The digest excludes these from time-based views.
