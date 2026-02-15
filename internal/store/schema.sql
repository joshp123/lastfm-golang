-- scrobbles schema v1

PRAGMA journal_mode=WAL;

CREATE TABLE IF NOT EXISTS scrobbles (
  played_at_uts INTEGER NOT NULL,
  track_name TEXT NOT NULL,
  artist_name TEXT NOT NULL,
  album_name TEXT,

  track_mbid TEXT,
  artist_mbid TEXT,
  album_mbid TEXT,

  lastfm_url TEXT,

  source_hash TEXT NOT NULL UNIQUE
);

CREATE INDEX IF NOT EXISTS idx_scrobbles_played_at_uts ON scrobbles(played_at_uts);
