package store

import "testing"

func TestStableSourceHashDeterministic(t *testing.T) {
	h1 := StableSourceHash(123, "artist", "track", "album")
	h2 := StableSourceHash(123, "artist", "track", "album")
	if h1 != h2 {
		t.Fatalf("expected deterministic hash: %q != %q", h1, h2)
	}
}
