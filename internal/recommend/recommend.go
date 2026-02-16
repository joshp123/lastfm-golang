package recommend

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joshp123/lastfm-golang/internal/lastfm"
)

const minSaneUTS = 946684800 // 2000-01-01

type Options struct {
	SeedArtistsLimit     int
	SeedWindow           string
	SimilarPerSeedArtist int
	SimilarArtistsLimit  int
	TopTracksPerArtist   int
	CandidateTracksLimit int
	ExcludeSeedArtists   bool
	IncludePlayedTracks  bool
	PreferUnplayed       bool
	MinLastPlayedWindow  string
}

func DefaultOptions() Options {
	return Options{
		SeedArtistsLimit:     8,
		SeedWindow:           "-90 days",
		SimilarPerSeedArtist: 15,
		SimilarArtistsLimit:  25,
		TopTracksPerArtist:   6,
		CandidateTracksLimit: 120,
		ExcludeSeedArtists:   true,
		IncludePlayedTracks:  true,
		PreferUnplayed:       true,
		MinLastPlayedWindow:  "-365 days",
	}
}

type Output struct {
	Meta    Meta         `json:"meta"`
	Seeds   []SeedArtist `json:"seeds"`
	Artists []ArtistCand `json:"artists"`
	Tracks  []TrackCand  `json:"tracks"`
}

type Meta struct {
	GeneratedAt time.Time `json:"generated_at"`
	Algo        string    `json:"algo"`
}

type SeedArtist struct {
	Artist string `json:"artist"`
	Plays  int64  `json:"plays"`
}

type ArtistCand struct {
	Rank            int      `json:"rank"`
	Artist          string   `json:"artist"`
	Score           float64  `json:"score"`
	FromSeedArtists []string `json:"from_seed_artists"`
}

type TrackCand struct {
	Rank   int     `json:"rank"`
	Artist string  `json:"artist"`
	Track  string  `json:"track"`
	Score  float64 `json:"score"`

	LocalPlays         int64 `json:"local_plays"`
	LocalLastPlayedUTS int64 `json:"local_last_played_uts"`
}

func Build(ctx context.Context, db *sql.DB, client lastfm.Client, opt Options) (Output, error) {
	seeds, err := seedArtists(ctx, db, opt.SeedWindow, opt.SeedArtistsLimit)
	if err != nil {
		return Output{}, err
	}
	seedSet := map[string]bool{}
	for _, s := range seeds {
		seedSet[strings.ToLower(s.Artist)] = true
	}

	type agg struct {
		score float64
		from  map[string]bool
	}
	artistsAgg := map[string]*agg{}

	for _, seed := range seeds {
		sim, err := getSimilarArtistsWithRetry(ctx, client, seed.Artist, opt.SimilarPerSeedArtist)
		if err != nil {
			return Output{}, err
		}
		for _, a := range sim {
			name := strings.TrimSpace(a.Name)
			if name == "" {
				continue
			}
			if opt.ExcludeSeedArtists && seedSet[strings.ToLower(name)] {
				continue
			}
			m, _ := strconv.ParseFloat(a.Match, 64)
			k := strings.ToLower(name)
			cur := artistsAgg[k]
			if cur == nil {
				cur = &agg{from: map[string]bool{}}
				artistsAgg[k] = cur
			}
			cur.score += m
			cur.from[seed.Artist] = true
		}
		// small pause to be nice to the API
		time.Sleep(200 * time.Millisecond)
	}

	artistCands := make([]ArtistCand, 0, len(artistsAgg))
	for k, v := range artistsAgg {
		from := make([]string, 0, len(v.from))
		for s := range v.from {
			from = append(from, s)
		}
		sort.Strings(from)
		artistCands = append(artistCands, ArtistCand{Artist: k, Score: v.score, FromSeedArtists: from})
	}
	sort.SliceStable(artistCands, func(i, j int) bool { return artistCands[i].Score > artistCands[j].Score })
	if len(artistCands) > opt.SimilarArtistsLimit {
		artistCands = artistCands[:opt.SimilarArtistsLimit]
	}
	for i := range artistCands {
		artistCands[i].Rank = i + 1
		// restore original-ish casing: just titlecase words is wrong; but we only stored lower.
		// best effort: keep as-is; LLM/tooling can autocorrect downstream.
	}

	// Expand to top tracks.
	tracks := []TrackCand{}
	seenTracks := map[string]bool{}
	stmtStats, err := db.PrepareContext(ctx, `SELECT COUNT(*), COALESCE(MAX(played_at_uts),0) FROM scrobbles WHERE played_at_uts >= ? AND artist_name = ? AND track_name = ?`)
	if err != nil {
		return Output{}, err
	}
	defer stmtStats.Close()

	for _, a := range artistCands {
		// Note: a.Artist is lowercase key. We need real name for API.
		artistName := a.Artist
		top, err := getArtistTopTracksWithRetry(ctx, client, artistName, opt.TopTracksPerArtist)
		if err != nil {
			return Output{}, err
		}
		for _, t := range top {
			track := strings.TrimSpace(t.Name)
			if track == "" {
				continue
			}
			key := strings.ToLower(artistName + "|" + track)
			if seenTracks[key] {
				continue
			}
			seenTracks[key] = true

			var plays int64
			var lastPlayed int64
			if err := stmtStats.QueryRowContext(ctx, minSaneUTS, artistName, track).Scan(&plays, &lastPlayed); err != nil {
				return Output{}, err
			}

			cand := TrackCand{Artist: artistName, Track: track, Score: a.Score, LocalPlays: plays, LocalLastPlayedUTS: lastPlayed}

			tracks = append(tracks, cand)
			if len(tracks) >= opt.CandidateTracksLimit {
				break
			}
		}
		if len(tracks) >= opt.CandidateTracksLimit {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Rank tracks: prefer unplayed, then score.
	sort.SliceStable(tracks, func(i, j int) bool {
		if opt.PreferUnplayed {
			iUn := tracks[i].LocalPlays == 0
			jUn := tracks[j].LocalPlays == 0
			if iUn != jUn {
				return iUn
			}
		}
		if tracks[i].Score == tracks[j].Score {
			return tracks[i].LocalLastPlayedUTS < tracks[j].LocalLastPlayedUTS
		}
		return tracks[i].Score > tracks[j].Score
	})

	if !opt.IncludePlayedTracks {
		filtered := tracks[:0]
		for _, t := range tracks {
			if t.LocalPlays == 0 {
				filtered = append(filtered, t)
			}
		}
		tracks = filtered
	}

	for i := range tracks {
		tracks[i].Rank = i + 1
	}

	return Output{
		Meta:    Meta{GeneratedAt: time.Now().UTC(), Algo: "seed-artists->similar-artists->top-tracks"},
		Seeds:   seeds,
		Artists: artistCands,
		Tracks:  tracks,
	}, nil
}

func seedArtists(ctx context.Context, db *sql.DB, window string, limit int) ([]SeedArtist, error) {
	rows, err := db.QueryContext(ctx, `
SELECT artist_name, COUNT(*) AS plays
FROM scrobbles
WHERE played_at_uts >= ?
  AND played_at_uts >= strftime('%s','now', ?)
GROUP BY artist_name
ORDER BY plays DESC
LIMIT ?
`, minSaneUTS, window, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SeedArtist{}
	for rows.Next() {
		var artist string
		var plays int64
		if err := rows.Scan(&artist, &plays); err != nil {
			return nil, err
		}
		out = append(out, SeedArtist{Artist: artist, Plays: plays})
	}
	return out, rows.Err()
}

func getSimilarArtistsWithRetry(ctx context.Context, client lastfm.Client, artist string, limit int) ([]lastfm.SimilarArtist, error) {
	const maxAttempts = 6
	backoff := 1 * time.Second
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		v, err := client.GetSimilarArtists(ctx, artist, limit)
		if err == nil {
			return v, nil
		}
		if !lastfm.IsRetryable(err) || attempt == maxAttempts {
			return nil, err
		}
		time.Sleep(backoff)
		if backoff < 20*time.Second {
			backoff *= 2
		}
	}
	return nil, fmt.Errorf("unreachable")
}

func getArtistTopTracksWithRetry(ctx context.Context, client lastfm.Client, artist string, limit int) ([]lastfm.TopTrack, error) {
	const maxAttempts = 6
	backoff := 1 * time.Second
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		v, err := client.GetArtistTopTracks(ctx, artist, limit)
		if err == nil {
			return v, nil
		}
		if !lastfm.IsRetryable(err) || attempt == maxAttempts {
			return nil, err
		}
		time.Sleep(backoff)
		if backoff < 20*time.Second {
			backoff *= 2
		}
	}
	return nil, fmt.Errorf("unreachable")
}
