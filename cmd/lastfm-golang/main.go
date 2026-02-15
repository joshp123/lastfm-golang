package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joshp123/lastfm-golang/internal/config"
	"github.com/joshp123/lastfm-golang/internal/lastfm"
	"github.com/joshp123/lastfm-golang/internal/logx"
	"github.com/joshp123/lastfm-golang/internal/store"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	cmd := args[0]
	if cmd == "-h" || cmd == "--help" || cmd == "help" {
		usage(os.Stdout)
		return 0
	}
	if cmd == "version" || cmd == "--version" || cmd == "-version" {
		fmt.Fprintln(os.Stdout, version)
		return 0
	}

	// subcommand flag parsing (single shared flagset for now)
	subArgs := args[1:]
	for _, a := range subArgs {
		if a == "--help" || a == "-h" || a == "-help" {
			usage(os.Stdout)
			return 0
		}
	}

	req := config.Requirements{}
	switch cmd {
	case "backfill", "sync":
		req.RequireAPIKey = true
		req.RequireUsername = true
	case "verify":
		// local only
	default:
		fmt.Fprintln(os.Stderr, "error: unknown command:", cmd)
		usage(os.Stderr)
		return 2
	}

	c, err := config.FromFlags(subArgs, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	log := logx.Logger{Out: os.Stderr, Verbose: c.Verbose}

	ctx := context.Background()
	s, err := store.Open(ctx, store.OpenOptions{DataDir: c.DataDir})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer s.Close()

	switch cmd {
	case "backfill":
		client := lastfm.Client{APIKey: c.APIKey, Username: c.Username, UserAgent: c.UserAgent}
		return cmdBackfill(ctx, log, client, s)
	case "sync":
		client := lastfm.Client{APIKey: c.APIKey, Username: c.Username, UserAgent: c.UserAgent}
		return cmdSync(ctx, log, client, s)
	case "verify":
		return cmdVerify(ctx, log, s)
	default:
		fmt.Fprintln(os.Stderr, "error: unknown command:", cmd)
		usage(os.Stderr)
		return 2
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `lastfm-golang

Usage:
  lastfm-golang <command> [flags]

Commands:
  backfill   Fetch all scrobbles and store (raw JSONL + SQLite)
  sync       Fetch new scrobbles since the last run
  verify     Print basic DB stats
  version    Print version

Flags (common):
  --env-file <path>         Load env vars from a file (or set LASTFM_ENV_FILE)
  --api-key <key>           Last.fm API key (or set LASTFM_API_KEY)
  --shared-secret <secret>  Last.fm shared secret (optional; or set LASTFM_SHARED_SECRET)
  --user <username>         Last.fm username (or set LASTFM_USERNAME)
  --data-dir <path>         Data directory (default: XDG data dir)
  --verbose                 Verbose logging (prints per-page progress)
  --user-agent <ua>         HTTP User-Agent

Help:
  lastfm-golang --help
`)
}

func cmdBackfill(ctx context.Context, log logx.Logger, client lastfm.Client, s *store.Store) int {
	const limit = 200
	page := 1
	totalPages := -1
	inserted := 0
	ignored := 0
	lastProgress := time.Now()

	for {
		p, err := client.GetRecentTracksPage(ctx, page, limit)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if totalPages == -1 {
			totalPages = p.TotalPages
			if totalPages == 0 {
				totalPages = 1
			}
			log.Infof("backfill: total scrobbles=%d totalPages=%d", p.Total, totalPages)
		}

		if len(p.Tracks) == 0 {
			break
		}

		for _, t := range p.Tracks {
			res, err := s.InsertScrobble(ctx, t)
			if err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				return 1
			}
			if res.Inserted > 0 {
				// Store raw once per unique scrobble; avoids ballooning JSONL on reruns.
				if err := s.AppendRaw(t); err != nil {
					fmt.Fprintln(os.Stderr, "error:", err)
					return 1
				}
			}
			inserted += res.Inserted
			ignored += res.Ignored
		}
		if err := s.RawJSONLBuf.Flush(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}

		log.Debugf("backfill: page %d/%d (inserted=%d ignored=%d)", page, totalPages, inserted, ignored)
		if !log.Verbose && time.Since(lastProgress) > 15*time.Second {
			log.Infof("backfill: page %d/%d (inserted=%d ignored=%d)", page, totalPages, inserted, ignored)
			lastProgress = time.Now()
		}

		if totalPages != -1 && page >= totalPages {
			break
		}
		page++
		time.Sleep(250 * time.Millisecond)
	}

	log.Infof("backfill done: inserted=%d ignored=%d", inserted, ignored)
	return 0
}

func cmdSync(ctx context.Context, log logx.Logger, client lastfm.Client, s *store.Store) int {
	const limit = 200
	maxSeen, err := s.MaxPlayedAtUTS(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	log.Infof("sync: max_played_at_uts=%d", maxSeen)

	page := 1
	inserted := 0
	ignored := 0
	stop := false
	lastProgress := time.Now()

	for {
		p, err := client.GetRecentTracksPage(ctx, page, limit)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if len(p.Tracks) == 0 {
			break
		}

		for _, t := range p.Tracks {
			res, err := s.InsertScrobble(ctx, t)
			if err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				return 1
			}
			if res.Inserted > 0 {
				if err := s.AppendRaw(t); err != nil {
					fmt.Fprintln(os.Stderr, "error:", err)
					return 1
				}
			}
			inserted += res.Inserted
			ignored += res.Ignored

			if t.Date != nil && t.Date.UTS != "" {
				uts, err := parseI64(t.Date.UTS)
				if err == nil && maxSeen != 0 && uts <= maxSeen {
					stop = true
				}
			}
		}
		if err := s.RawJSONLBuf.Flush(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}

		log.Debugf("sync: page %d (inserted=%d ignored=%d)", page, inserted, ignored)
		if !log.Verbose && time.Since(lastProgress) > 15*time.Second {
			log.Infof("sync: page %d (inserted=%d ignored=%d)", page, inserted, ignored)
			lastProgress = time.Now()
		}
		if stop {
			break
		}
		page++
		time.Sleep(250 * time.Millisecond)
	}

	log.Infof("sync done: inserted=%d ignored=%d", inserted, ignored)
	return 0
}

func cmdVerify(ctx context.Context, log logx.Logger, s *store.Store) int {
	count, minUTS, maxUTS, err := s.Stats(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	_ = log // reserved for future diagnostics
	fmt.Fprintf(os.Stdout, "scrobbles: count=%d min_uts=%d max_uts=%d\n", count, minUTS, maxUTS)
	return 0
}

func parseI64(s string) (int64, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid uts: %q", s)
	}
	return v, nil
}
