package lastfm

import (
	"context"
	"net/url"
	"strconv"
)

type SimilarArtistsResponse struct {
	SimilarArtists struct {
		Artist []SimilarArtist `json:"artist"`
	} `json:"similarartists"`

	Error   int    `json:"error"`
	Message string `json:"message"`
}

type SimilarArtist struct {
	Name  string `json:"name"`
	Match string `json:"match"`
	URL   string `json:"url"`
	MBID  string `json:"mbid"`
}

type TopTracksResponse struct {
	TopTracks struct {
		Track []TopTrack `json:"track"`
	} `json:"toptracks"`

	Error   int    `json:"error"`
	Message string `json:"message"`
}

type TopTrack struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	MBID string `json:"mbid"`
}

func (c Client) GetSimilarArtists(ctx context.Context, artist string, limit int) ([]SimilarArtist, error) {
	q := url.Values{}
	q.Set("method", "artist.getSimilar")
	q.Set("artist", artist)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("autocorrect", "1")

	var r SimilarArtistsResponse
	if err := c.doGet(ctx, q, &r); err != nil {
		return nil, err
	}
	if r.Error != 0 {
		return nil, APIError{Code: r.Error, Message: r.Message}
	}
	return r.SimilarArtists.Artist, nil
}

func (c Client) GetArtistTopTracks(ctx context.Context, artist string, limit int) ([]TopTrack, error) {
	q := url.Values{}
	q.Set("method", "artist.getTopTracks")
	q.Set("artist", artist)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("autocorrect", "1")

	var r TopTracksResponse
	if err := c.doGet(ctx, q, &r); err != nil {
		return nil, err
	}
	if r.Error != 0 {
		return nil, APIError{Code: r.Error, Message: r.Message}
	}
	return r.TopTracks.Track, nil
}
