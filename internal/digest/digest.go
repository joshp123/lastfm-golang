package digest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

const minSaneUTS = 946684800 // 2000-01-01

type Digest struct {
	Meta      Meta       `json:"meta"`
	Recent    []Scrobble `json:"recent"`
	Top       Top        `json:"top"`
	Resurface Resurface  `json:"resurface"`
	Yearly    Yearly     `json:"yearly"`
	Signature Signature  `json:"signature"`
}

type Meta struct {
	GeneratedAt      time.Time `json:"generated_at"`
	ScrobblesTotal   int64     `json:"scrobbles_total"`
	ScrobblesDated   int64     `json:"scrobbles_dated"`
	ScrobblesSuspect int64     `json:"scrobbles_suspect"`
	DatedMinUTS      int64     `json:"dated_min_uts"`
	DatedMaxUTS      int64     `json:"dated_max_uts"`
}

type Scrobble struct {
	PlayedAtUTS int64  `json:"played_at_uts"`
	PlayedAt    string `json:"played_at"`
	Artist      string `json:"artist"`
	Track       string `json:"track"`
	Album       string `json:"album,omitempty"`
}

type RankedArtist struct {
	Rank   int    `json:"rank"`
	Artist string `json:"artist"`
	Plays  int64  `json:"plays"`
}

type RankedTrack struct {
	Rank          int    `json:"rank"`
	Artist        string `json:"artist"`
	Track         string `json:"track"`
	Plays         int64  `json:"plays"`
	LastPlayedUTS int64  `json:"last_played_uts"`
}

type RankedAlbum struct {
	Rank          int    `json:"rank"`
	Artist        string `json:"artist"`
	Album         string `json:"album"`
	Plays         int64  `json:"plays"`
	LastPlayedUTS int64  `json:"last_played_uts"`
}

type YearlyArtist struct {
	Year   int    `json:"year"`
	Rank   int    `json:"rank"`
	Artist string `json:"artist"`
	Plays  int64  `json:"plays"`
}

type SignatureArtist struct {
	Rank            int    `json:"rank"`
	Artist          string `json:"artist"`
	YearsInTop      int64  `json:"years_in_top"`
	FirstYear       int    `json:"first_year"`
	LastYear        int    `json:"last_year"`
	PlaysInTopYears int64  `json:"plays_in_top_years"`
}

type Top struct {
	Artists30d  []RankedArtist `json:"artists_30d"`
	Artists365d []RankedArtist `json:"artists_365d"`
	Tracks30d   []RankedTrack  `json:"tracks_30d"`
	Albums30d   []RankedAlbum  `json:"albums_30d"`
}

type Resurface struct {
	Tracks180d []RankedTrack `json:"tracks_180d"`
	Albums180d []RankedAlbum `json:"albums_180d"`
}

type Yearly struct {
	TopArtists []YearlyArtist `json:"top_artists"`
}

type Signature struct {
	Artists []SignatureArtist `json:"artists"`
}

type Options struct {
	RecentLimit             int
	TopArtistsLimit         int
	TopTracksLimit          int
	TopAlbumsLimit          int
	YearlyTopArtistsPerYear int
	SignatureLimit          int
	SignatureMinYears       int
}

func DefaultOptions() Options {
	return Options{
		RecentLimit:             150,
		TopArtistsLimit:         25,
		TopTracksLimit:          50,
		TopAlbumsLimit:          40,
		YearlyTopArtistsPerYear: 10,
		SignatureLimit:          50,
		SignatureMinYears:       5,
	}
}

func Build(ctx context.Context, db *sql.DB, opt Options) (Digest, error) {
	if opt.RecentLimit <= 0 || opt.RecentLimit > 1000 {
		return Digest{}, fmt.Errorf("invalid RecentLimit: %d", opt.RecentLimit)
	}

	meta, err := computeMeta(ctx, db)
	if err != nil {
		return Digest{}, err
	}

	recent, err := recentScrobbles(ctx, db, opt.RecentLimit)
	if err != nil {
		return Digest{}, err
	}

	topArtists30d, err := topArtists(ctx, db, "-30 days", opt.TopArtistsLimit)
	if err != nil {
		return Digest{}, err
	}
	topArtists365d, err := topArtists(ctx, db, "-365 days", opt.TopArtistsLimit)
	if err != nil {
		return Digest{}, err
	}
	topTracks30d, err := topTracks(ctx, db, "-30 days", opt.TopTracksLimit)
	if err != nil {
		return Digest{}, err
	}
	topAlbums30d, err := topAlbums(ctx, db, "-30 days", opt.TopAlbumsLimit)
	if err != nil {
		return Digest{}, err
	}

	resurfaceTracks180d, err := resurfaceTracks(ctx, db, "-180 days", opt.TopTracksLimit)
	if err != nil {
		return Digest{}, err
	}
	resurfaceAlbums180d, err := resurfaceAlbums(ctx, db, "-180 days", opt.TopAlbumsLimit)
	if err != nil {
		return Digest{}, err
	}

	yearlyTopArtists, err := yearlyTopArtists(ctx, db, opt.YearlyTopArtistsPerYear)
	if err != nil {
		return Digest{}, err
	}

	signatureArtists, err := signatureArtists(ctx, db, opt.SignatureMinYears, opt.SignatureLimit)
	if err != nil {
		return Digest{}, err
	}

	return Digest{
		Meta:   meta,
		Recent: recent,
		Top: Top{
			Artists30d:  topArtists30d,
			Artists365d: topArtists365d,
			Tracks30d:   topTracks30d,
			Albums30d:   topAlbums30d,
		},
		Resurface: Resurface{
			Tracks180d: resurfaceTracks180d,
			Albums180d: resurfaceAlbums180d,
		},
		Yearly:    Yearly{TopArtists: yearlyTopArtists},
		Signature: Signature{Artists: signatureArtists},
	}, nil
}

func EncodeJSON(v any, pretty bool) ([]byte, error) {
	if pretty {
		return json.MarshalIndent(v, "", "  ")
	}
	return json.Marshal(v)
}

func computeMeta(ctx context.Context, db *sql.DB) (Meta, error) {
	var total int64
	var dated int64
	var suspect int64
	var datedMin sql.NullInt64
	var datedMax sql.NullInt64

	if err := db.QueryRowContext(ctx, `
SELECT
  COUNT(*) AS total,
  SUM(CASE WHEN played_at_uts >= ? THEN 1 ELSE 0 END) AS dated,
  SUM(CASE WHEN played_at_uts < ? THEN 1 ELSE 0 END) AS suspect,
  MIN(CASE WHEN played_at_uts >= ? THEN played_at_uts ELSE NULL END) AS dated_min,
  MAX(CASE WHEN played_at_uts >= ? THEN played_at_uts ELSE NULL END) AS dated_max
FROM scrobbles
`, minSaneUTS, minSaneUTS, minSaneUTS, minSaneUTS).Scan(&total, &dated, &suspect, &datedMin, &datedMax); err != nil {
		return Meta{}, err
	}

	return Meta{
		GeneratedAt:      time.Now().UTC(),
		ScrobblesTotal:   total,
		ScrobblesDated:   dated,
		ScrobblesSuspect: suspect,
		DatedMinUTS:      nullI64(datedMin),
		DatedMaxUTS:      nullI64(datedMax),
	}, nil
}

func recentScrobbles(ctx context.Context, db *sql.DB, limit int) ([]Scrobble, error) {
	rows, err := db.QueryContext(ctx, `
SELECT played_at_uts, artist_name, track_name, COALESCE(album_name, '')
FROM scrobbles
WHERE played_at_uts >= ?
ORDER BY played_at_uts DESC
LIMIT ?
`, minSaneUTS, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Scrobble{}
	for rows.Next() {
		var uts int64
		var artist, track, album string
		if err := rows.Scan(&uts, &artist, &track, &album); err != nil {
			return nil, err
		}
		s := Scrobble{PlayedAtUTS: uts, PlayedAt: time.Unix(uts, 0).UTC().Format(time.RFC3339), Artist: artist, Track: track}
		if album != "" {
			s.Album = album
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func topArtists(ctx context.Context, db *sql.DB, window string, limit int) ([]RankedArtist, error) {
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

	out := []RankedArtist{}
	rank := 1
	for rows.Next() {
		var artist string
		var plays int64
		if err := rows.Scan(&artist, &plays); err != nil {
			return nil, err
		}
		out = append(out, RankedArtist{Rank: rank, Artist: artist, Plays: plays})
		rank++
	}
	return out, rows.Err()
}

func topTracks(ctx context.Context, db *sql.DB, window string, limit int) ([]RankedTrack, error) {
	rows, err := db.QueryContext(ctx, `
SELECT artist_name, track_name, COUNT(*) AS plays, MAX(played_at_uts) AS last_played
FROM scrobbles
WHERE played_at_uts >= ?
  AND played_at_uts >= strftime('%s','now', ?)
GROUP BY artist_name, track_name
ORDER BY plays DESC
LIMIT ?
`, minSaneUTS, window, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RankedTrack{}
	rank := 1
	for rows.Next() {
		var artist, track string
		var plays, lastPlayed int64
		if err := rows.Scan(&artist, &track, &plays, &lastPlayed); err != nil {
			return nil, err
		}
		out = append(out, RankedTrack{Rank: rank, Artist: artist, Track: track, Plays: plays, LastPlayedUTS: lastPlayed})
		rank++
	}
	return out, rows.Err()
}

func topAlbums(ctx context.Context, db *sql.DB, window string, limit int) ([]RankedAlbum, error) {
	rows, err := db.QueryContext(ctx, `
SELECT artist_name, album_name, COUNT(*) AS plays, MAX(played_at_uts) AS last_played
FROM scrobbles
WHERE played_at_uts >= ?
  AND played_at_uts >= strftime('%s','now', ?)
  AND album_name IS NOT NULL
  AND album_name != ''
GROUP BY artist_name, album_name
ORDER BY plays DESC
LIMIT ?
`, minSaneUTS, window, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RankedAlbum{}
	rank := 1
	for rows.Next() {
		var artist, album string
		var plays, lastPlayed int64
		if err := rows.Scan(&artist, &album, &plays, &lastPlayed); err != nil {
			return nil, err
		}
		out = append(out, RankedAlbum{Rank: rank, Artist: artist, Album: album, Plays: plays, LastPlayedUTS: lastPlayed})
		rank++
	}
	return out, rows.Err()
}

func resurfaceTracks(ctx context.Context, db *sql.DB, staleWindow string, limit int) ([]RankedTrack, error) {
	rows, err := db.QueryContext(ctx, `
SELECT artist_name, track_name, COUNT(*) AS plays, MAX(played_at_uts) AS last_played
FROM scrobbles
WHERE played_at_uts >= ?
GROUP BY artist_name, track_name
HAVING last_played < strftime('%s','now', ?)
ORDER BY plays DESC
LIMIT ?
`, minSaneUTS, staleWindow, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RankedTrack{}
	rank := 1
	for rows.Next() {
		var artist, track string
		var plays, lastPlayed int64
		if err := rows.Scan(&artist, &track, &plays, &lastPlayed); err != nil {
			return nil, err
		}
		out = append(out, RankedTrack{Rank: rank, Artist: artist, Track: track, Plays: plays, LastPlayedUTS: lastPlayed})
		rank++
	}
	return out, rows.Err()
}

func resurfaceAlbums(ctx context.Context, db *sql.DB, staleWindow string, limit int) ([]RankedAlbum, error) {
	rows, err := db.QueryContext(ctx, `
SELECT artist_name, album_name, COUNT(*) AS plays, MAX(played_at_uts) AS last_played
FROM scrobbles
WHERE played_at_uts >= ?
  AND album_name IS NOT NULL
  AND album_name != ''
GROUP BY artist_name, album_name
HAVING last_played < strftime('%s','now', ?)
ORDER BY plays DESC
LIMIT ?
`, minSaneUTS, staleWindow, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RankedAlbum{}
	rank := 1
	for rows.Next() {
		var artist, album string
		var plays, lastPlayed int64
		if err := rows.Scan(&artist, &album, &plays, &lastPlayed); err != nil {
			return nil, err
		}
		out = append(out, RankedAlbum{Rank: rank, Artist: artist, Album: album, Plays: plays, LastPlayedUTS: lastPlayed})
		rank++
	}
	return out, rows.Err()
}

func yearlyTopArtists(ctx context.Context, db *sql.DB, perYear int) ([]YearlyArtist, error) {
	// Window function requires reasonably modern SQLite (modernc provides it).
	rows, err := db.QueryContext(ctx, `
WITH yearly AS (
  SELECT
    CAST(strftime('%Y', played_at_uts, 'unixepoch') AS INTEGER) AS year,
    artist_name,
    COUNT(*) AS plays
  FROM scrobbles
  WHERE played_at_uts >= ?
  GROUP BY year, artist_name
),
ranked AS (
  SELECT year, artist_name, plays,
         ROW_NUMBER() OVER (PARTITION BY year ORDER BY plays DESC) AS rnk
  FROM yearly
)
SELECT year, rnk, artist_name, plays
FROM ranked
WHERE rnk <= ?
ORDER BY year ASC, rnk ASC
`, minSaneUTS, perYear)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []YearlyArtist{}
	for rows.Next() {
		var year int
		var rank int
		var artist string
		var plays int64
		if err := rows.Scan(&year, &rank, &artist, &plays); err != nil {
			return nil, err
		}
		out = append(out, YearlyArtist{Year: year, Rank: rank, Artist: artist, Plays: plays})
	}
	return out, rows.Err()
}

func signatureArtists(ctx context.Context, db *sql.DB, minYears int, limit int) ([]SignatureArtist, error) {
	rows, err := db.QueryContext(ctx, `
WITH yearly AS (
  SELECT
    CAST(strftime('%Y', played_at_uts, 'unixepoch') AS INTEGER) AS year,
    artist_name,
    COUNT(*) AS plays
  FROM scrobbles
  WHERE played_at_uts >= ?
  GROUP BY year, artist_name
),
ranked AS (
  SELECT year, artist_name, plays,
         ROW_NUMBER() OVER (PARTITION BY year ORDER BY plays DESC) AS rnk
  FROM yearly
),
top AS (
  SELECT year, artist_name, plays
  FROM ranked
  WHERE rnk <= 20
),
agg AS (
  SELECT
    artist_name,
    COUNT(DISTINCT year) AS years_in_top,
    MIN(year) AS first_year,
    MAX(year) AS last_year,
    SUM(plays) AS plays_in_top_years
  FROM top
  GROUP BY artist_name
  HAVING years_in_top >= ?
)
SELECT artist_name, years_in_top, first_year, last_year, plays_in_top_years
FROM agg
ORDER BY years_in_top DESC, plays_in_top_years DESC
LIMIT ?
`, minSaneUTS, minYears, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SignatureArtist{}
	rank := 1
	for rows.Next() {
		var artist string
		var yearsInTop int64
		var firstYear, lastYear int
		var plays int64
		if err := rows.Scan(&artist, &yearsInTop, &firstYear, &lastYear, &plays); err != nil {
			return nil, err
		}
		out = append(out, SignatureArtist{Rank: rank, Artist: artist, YearsInTop: yearsInTop, FirstYear: firstYear, LastYear: lastYear, PlaysInTopYears: plays})
		rank++
	}
	return out, rows.Err()
}

func nullI64(v sql.NullInt64) int64 {
	if !v.Valid {
		return 0
	}
	return v.Int64
}
