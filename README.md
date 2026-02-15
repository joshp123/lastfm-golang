# lastfm-golang

A tiny CLI to dump your Last.fm scrobble history locally (raw JSONL + SQLite), then keep it synced daily.

## Install

```bash
# install from the main package
go install github.com/joshp123/lastfm-golang/cmd/lastfm-golang@latest
```

## Quick start

Set env (or use `--env-file`):

```bash
export LASTFM_API_KEY="..."
export LASTFM_USERNAME="joshpalmer"
```

Or:

```bash
lastfm-golang backfill --env-file /path/to/lastfm.env
```

Backfill everything (can take a while):

```bash
lastfm-golang backfill
```

Daily incremental sync:

```bash
lastfm-golang sync
```

Verify DB stats:

```bash
lastfm-golang verify
```

## Data location

Defaults to:

- `${XDG_DATA_HOME:-~/.local/share}/lastfm-golang/`
  - `scrobbles.raw.jsonl`
  - `lastfm.sqlite`

Override with `--data-dir`.

## Notes

- This uses Last.fm `user.getRecentTracks`.
- "Now playing" items are ignored (they have no `date.uts`).
- Some historic scrobbles may have placeholder 1970 timestamps from Last.fm; `verify` reports these as `scrobbles_suspect`.
- Inserts are idempotent via a stable `source_hash` unique key.
