package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joshp123/lastfm-golang/internal/config"
	"github.com/joshp123/lastfm-golang/internal/digest"
	"github.com/joshp123/lastfm-golang/internal/lastfm"
	"github.com/joshp123/lastfm-golang/internal/logx"
	"github.com/joshp123/lastfm-golang/internal/recommend"
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
	case "recommend":
		req.RequireAPIKey = true
		// username not required for recommend
	case "verify", "digest":
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
	case "digest":
		return cmdDigest(ctx, log, s)
	case "recommend":
		client := lastfm.Client{APIKey: c.APIKey, UserAgent: c.UserAgent}
		return cmdRecommend(ctx, log, client, s)
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
  backfill    Fetch all scrobbles and store (raw JSONL + SQLite)
  sync        Fetch new scrobbles since the last run
  verify      Print basic DB stats
  digest      Print an LLM-friendly JSON digest (recent + top + yearly)
  recommend   Print LLM-friendly JSON track candidates for discovery
  version     Print version

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
		p, err := getPageWithRetry(ctx, log, client, page, limit)
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
		p, err := getPageWithRetry(ctx, log, client, page, limit)
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
	_ = log // reserved for future diagnostics

	const minSaneUTS = 946684800 // 2000-01-01; Last.fm can return 1970 placeholders for unknown timestamps.

	count, minUTS, maxUTS, err := s.Stats(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	var suspectCount int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM scrobbles WHERE played_at_uts < ?`, minSaneUTS).Scan(&suspectCount); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	var datedCount int64
	var datedMin sql.NullInt64
	var datedMax sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*), MIN(played_at_uts), MAX(played_at_uts) FROM scrobbles WHERE played_at_uts >= ?`, minSaneUTS).Scan(&datedCount, &datedMin, &datedMax); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	fmt.Fprintf(
		os.Stdout,
		"scrobbles_total=%d scrobbles_dated=%d scrobbles_suspect=%d min_uts=%d max_uts=%d dated_min_uts=%d dated_max_uts=%d\n",
		count,
		datedCount,
		suspectCount,
		minUTS,
		maxUTS,
		nullI64(datedMin),
		nullI64(datedMax),
	)
	return 0
}

func cmdDigest(ctx context.Context, log logx.Logger, s *store.Store) int {
	_ = log // reserved for future diagnostics

	opt := digest.DefaultOptions()
	out, err := digest.Build(ctx, s.DB, opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	b, err := digest.EncodeJSON(out, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if _, err := os.Stdout.Write(append(b, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func cmdRecommend(ctx context.Context, log logx.Logger, client lastfm.Client, s *store.Store) int {
	_ = log // reserved for future diagnostics

	opt := recommend.DefaultOptions()
	out, err := recommend.Build(ctx, s.DB, client, opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	b, err := recommend.EncodeJSON(out, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if _, err := os.Stdout.Write(append(b, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func getPageWithRetry(ctx context.Context, log logx.Logger, client lastfm.Client, page, limit int) (lastfm.Page, error) {
	const maxAttempts = 8
	backoff := 1 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		p, err := client.GetRecentTracksPage(ctx, page, limit)
		if err == nil {
			return p, nil
		}
		if !lastfm.IsRetryable(err) || attempt == maxAttempts {
			return lastfm.Page{}, err
		}

		log.Infof("retry: page %d attempt %d/%d: %v", page, attempt, maxAttempts, err)
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}

	return lastfm.Page{}, fmt.Errorf("unreachable")
}

func nullI64(v sql.NullInt64) int64 {
	if !v.Valid {
		return 0
	}
	return v.Int64
}

func parseI64(s string) (int64, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid uts: %q", s)
	}
	return v, nil
}
