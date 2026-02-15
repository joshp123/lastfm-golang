package store

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"

	"github.com/joshp123/lastfm-golang/internal/lastfm"
)

//go:embed schema.sql
var schemaFS embed.FS

type Store struct {
	DB          *sql.DB
	RawJSONL    *os.File
	RawJSONLBuf *bufio.Writer
}

type OpenOptions struct {
	DataDir string
}

func Open(ctx context.Context, opt OpenOptions) (*Store, error) {
	if err := os.MkdirAll(opt.DataDir, 0o755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(opt.DataDir, "lastfm.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	schemaBytes, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, string(schemaBytes)); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	rawPath := filepath.Join(opt.DataDir, "scrobbles.raw.jsonl")
	rawF, err := os.OpenFile(rawPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{DB: db, RawJSONL: rawF, RawJSONLBuf: bufio.NewWriterSize(rawF, 1024*1024)}, nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	if s.RawJSONLBuf != nil {
		_ = s.RawJSONLBuf.Flush()
	}
	if s.RawJSONL != nil {
		_ = s.RawJSONL.Close()
	}
	if s.DB != nil {
		_ = s.DB.Close()
	}
	return nil
}

type RawEnvelope struct {
	FetchedAt time.Time   `json:"fetched_at"`
	Track     lastfm.Track `json:"track"`
}

func (s *Store) AppendRaw(track lastfm.Track) error {
	e := RawEnvelope{FetchedAt: time.Now().UTC(), Track: track}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := s.RawJSONLBuf.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// StableSourceHash is the dedupe key for a scrobble.
func StableSourceHash(playedAtUTS int64, artist, track, album string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s", playedAtUTS, artist, track, album)))
	return hex.EncodeToString(h[:])
}

type InsertResult struct {
	Inserted int
	Ignored  int
}

func (s *Store) InsertScrobble(ctx context.Context, t lastfm.Track) (InsertResult, error) {
	if t.Date == nil || t.Date.UTS == "" {
		return InsertResult{Ignored: 1}, nil
	}
	playedAt, err := parseI64(t.Date.UTS)
	if err != nil {
		return InsertResult{}, err
	}

	artist := t.Artist.Text
	track := t.Name
	album := t.Album.Text
	hash := StableSourceHash(playedAt, artist, track, album)

	res, err := s.DB.ExecContext(ctx, `
INSERT OR IGNORE INTO scrobbles(
  played_at_uts, track_name, artist_name, album_name,
  track_mbid, artist_mbid, album_mbid,
  lastfm_url,
  source_hash
) VALUES(?,?,?,?,?,?,?,?,?)
`,
		playedAt, track, artist, nullIfEmpty(album),
		nullIfEmpty(t.MBID), nullIfEmpty(t.Artist.MBID), nullIfEmpty(t.Album.MBID),
		nullIfEmpty(t.URL),
		hash,
	)
	if err != nil {
		return InsertResult{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return InsertResult{Ignored: 1}, nil
	}
	return InsertResult{Inserted: 1}, nil
}

func (s *Store) MaxPlayedAtUTS(ctx context.Context) (int64, error) {
	var v sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, `SELECT MAX(played_at_uts) FROM scrobbles`).Scan(&v); err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return v.Int64, nil
}

func (s *Store) Stats(ctx context.Context) (count int64, minUTS int64, maxUTS int64, err error) {
	var c sql.NullInt64
	var min sql.NullInt64
	var max sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*), MIN(played_at_uts), MAX(played_at_uts) FROM scrobbles`).Scan(&c, &min, &max); err != nil {
		return 0, 0, 0, err
	}
	return c.Int64, min.Int64, max.Int64, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func parseI64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
